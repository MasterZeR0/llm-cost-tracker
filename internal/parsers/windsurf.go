package parsers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/tokenestimator"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/vscdb"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type WindsurfParser struct{}

func init() {
	Register(&WindsurfParser{})
}

func (p *WindsurfParser) Name() string {
	return "windsurf"
}

func (p *WindsurfParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if base == "state.vscdb" {
				sources = append(sources, DataSource{Path: path, Type: "sqlite"})
			} else if strings.HasSuffix(base, ".json") {
				sources = append(sources, DataSource{Path: path, Type: "json"})
			}
			return nil
		})
	}
	return sources, nil
}

type cascadeSessionJSON struct {
	SessionID string            `json:"session_id"`
	CascadeID string            `json:"cascade_id"`
	ID        string            `json:"id"`
	Model     string            `json:"model"`
	CreatedAt string            `json:"created_at"`
	Steps     []cascadeStepJSON `json:"steps"`
	Turns     []cascadeStepJSON `json:"turns"`
	Messages  []cascadeStepJSON `json:"messages"`
}

type cascadeStepJSON struct {
	StepIndex         int64             `json:"step_index"`
	Timestamp         string            `json:"timestamp"`
	CreatedAt         string            `json:"created_at"`
	Model             string            `json:"model"`
	UserPrompt        string            `json:"user_prompt"`
	Prompt            string            `json:"prompt"`
	UserMessage       string            `json:"user_message"`
	Query             string            `json:"query"`
	AssistantResponse string            `json:"assistant_response"`
	Response          string            `json:"response"`
	Content           string            `json:"content"`
	Text              string            `json:"text"`
	DurationMS        *int64            `json:"duration_ms"`
	LatencyMS         *int64            `json:"latency_ms"`
	CostUSD           *float64          `json:"cost_usd"`
	Cost              *float64          `json:"cost"`
	Tokens            *cascadeTokenJSON `json:"tokens"`
	InputTokens       int64             `json:"input_tokens"`
	OutputTokens      int64             `json:"output_tokens"`
	ToolCalls         []cascadeToolCall `json:"tool_calls"`
	ToolExecutions    []cascadeToolCall `json:"tool_executions"`
	Tools             []cascadeToolCall `json:"tools"`
}

type cascadeTokenJSON struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Input        int64 `json:"input"`
	Output       int64 `json:"output"`
}

type cascadeToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ToolName string `json:"tool_name"`
	Status   string `json:"status"`
	IsError  bool   `json:"is_error"`
	Error    string `json:"error"`
}

func (p *WindsurfParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	if source.Type == "sqlite" || strings.HasSuffix(source.Path, ".vscdb") {
		return p.parseVSCDB(source, state)
	}
	return p.parseJSON(source, state)
}

func (p *WindsurfParser) parseJSON(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	fi, err := os.Stat(source.Path)
	if err != nil {
		return nil, state, err
	}

	// Truncation detection: if file shrank below LastOffset, reset offset
	if state.LastOffset > 0 && fi.Size() < state.LastOffset {
		state.LastOffset = 0
		state.LastRowID = ""
	}

	// If offset matches file size, no new data has been written
	if state.LastOffset > 0 && fi.Size() == state.LastOffset {
		return nil, state, nil
	}

	data, err := os.ReadFile(source.Path)
	if err != nil {
		return nil, state, err
	}

	var session cascadeSessionJSON
	if err := json.Unmarshal(data, &session); err != nil {
		// Try unmarshaling as a slice of steps
		var steps []cascadeStepJSON
		if errSlice := json.Unmarshal(data, &steps); errSlice == nil {
			session.Steps = steps
		} else {
			// Incomplete or malformed JSON (e.g. active write in progress)
			// Do not advance LastOffset so the file can be retried on the next poll.
			return nil, state, fmt.Errorf("unmarshal cascade session %s: %w", source.Path, err)
		}
	}

	sessionID := session.SessionID
	if sessionID == "" {
		sessionID = session.CascadeID
	}
	if sessionID == "" {
		sessionID = session.ID
	}
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
	}

	steps := session.Steps
	if len(steps) == 0 {
		steps = session.Turns
	}
	if len(steps) == 0 {
		steps = session.Messages
	}

	lastStepIndex := int64(0)
	if state.LastOffset > 0 && state.LastRowID != "" {
		lastStepIndex, _ = strconv.ParseInt(state.LastRowID, 10, 64)
	}

	var requests []store.Request
	pricingEngine := pricing.NewEngine(nil)

	for i, step := range steps {
		stepIdx := step.StepIndex
		if stepIdx <= 0 {
			stepIdx = int64(i + 1)
		}

		if stepIdx <= lastStepIndex {
			continue
		}

		req := buildWindsurfRequest(source.Path, sessionID, session.Model, stepIdx, step, pricingEngine)
		requests = append(requests, req)

		if stepIdx > lastStepIndex {
			lastStepIndex = stepIdx
		}
	}

	state.LastOffset = fi.Size()
	state.LastRowID = strconv.FormatInt(lastStepIndex, 10)
	modTime := fi.ModTime()
	state.LastModified = &modTime

	return requests, state, nil
}

func (p *WindsurfParser) parseVSCDB(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	fi, err := os.Stat(source.Path)
	if err != nil {
		return nil, state, err
	}

	if state.LastModified != nil && !fi.ModTime().After(*state.LastModified) {
		return nil, state, nil
	}

	reader, err := vscdb.Open(source.Path)
	if err != nil {
		// Unable to open vscdb (e.g. invalid format or missing ItemTable) - return gracefully
		return nil, state, nil
	}
	defer reader.Close()

	keys, err := reader.GetKeysLike("cascade%")
	if err != nil || len(keys) == 0 {
		// Try workbench panel chat or windsurf prefix
		keys, err = reader.GetKeysLike("%windsurf%")
	}
	if err != nil || len(keys) == 0 {
		modTime := fi.ModTime()
		state.LastModified = &modTime
		return nil, state, nil
	}

	pricingEngine := pricing.NewEngine(nil)
	var requests []store.Request

	for _, val := range keys {
		if len(val) == 0 {
			continue
		}

		var session cascadeSessionJSON
		if err := json.Unmarshal(val, &session); err != nil {
			var sessions []cascadeSessionJSON
			if errSlice := json.Unmarshal(val, &sessions); errSlice == nil {
				for _, s := range sessions {
					reqs := processCascadeSession(source.Path, s, pricingEngine)
					requests = append(requests, reqs...)
				}
			}
			continue
		}

		reqs := processCascadeSession(source.Path, session, pricingEngine)
		requests = append(requests, reqs...)
	}

	modTime := fi.ModTime()
	state.LastModified = &modTime

	return requests, state, nil
}

func processCascadeSession(sourcePath string, session cascadeSessionJSON, pricingEngine *pricing.Engine) []store.Request {
	sessionID := session.SessionID
	if sessionID == "" {
		sessionID = session.CascadeID
	}
	if sessionID == "" {
		sessionID = session.ID
	}
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	}

	steps := session.Steps
	if len(steps) == 0 {
		steps = session.Turns
	}
	if len(steps) == 0 {
		steps = session.Messages
	}

	var requests []store.Request
	for i, step := range steps {
		stepIdx := step.StepIndex
		if stepIdx <= 0 {
			stepIdx = int64(i + 1)
		}
		req := buildWindsurfRequest(sourcePath, sessionID, session.Model, stepIdx, step, pricingEngine)
		requests = append(requests, req)
	}
	return requests
}

func buildWindsurfRequest(sourcePath, sessionID, defaultModel string, stepIdx int64, step cascadeStepJSON, pricingEngine *pricing.Engine) store.Request {
	model := step.Model
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		model = "cascade-base"
	}

	prompt := step.UserPrompt
	if prompt == "" {
		prompt = step.Prompt
	}
	if prompt == "" {
		prompt = step.UserMessage
	}
	if prompt == "" {
		prompt = step.Query
	}

	resp := step.AssistantResponse
	if resp == "" {
		resp = step.Response
	}
	if resp == "" {
		resp = step.Content
	}
	if resp == "" {
		resp = step.Text
	}

	var inTokens, outTokens int64
	if step.Tokens != nil {
		inTokens = step.Tokens.InputTokens + step.Tokens.Input
		outTokens = step.Tokens.OutputTokens + step.Tokens.Output
	} else {
		inTokens = step.InputTokens
		outTokens = step.OutputTokens
	}

	var status string
	if inTokens > 0 || outTokens > 0 {
		status = "success"
	} else {
		inTokens, outTokens = tokenestimator.EstimateTokens(prompt, resp)
		status = "estimated"
	}

	// Cost calculation
	var costUSD *float64
	if step.CostUSD != nil {
		costUSD = step.CostUSD
	} else if step.Cost != nil {
		costUSD = step.Cost
	} else {
		cost, ok := pricingEngine.CalculateCost(model, inTokens, outTokens, 0, 0)
		if ok {
			costUSD = cost
		}
	}

	// Latency
	var latencyMS *int64
	if step.DurationMS != nil {
		latencyMS = step.DurationMS
	} else if step.LatencyMS != nil {
		latencyMS = step.LatencyMS
	}

	// Timestamp parsing
	ts := time.Now().UTC()
	tsStr := step.Timestamp
	if tsStr == "" {
		tsStr = step.CreatedAt
	}
	if tsStr != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			ts = parsed.UTC()
		} else if parsed, err := time.Parse(time.RFC3339, tsStr); err == nil {
			ts = parsed.UTC()
		}
	}

	reqID := fmt.Sprintf("windsurf:%s:%d", sessionID, stepIdx)

	// Tool executions
	rawTools := append([]cascadeToolCall{}, step.ToolCalls...)
	rawTools = append(rawTools, step.ToolExecutions...)
	rawTools = append(rawTools, step.Tools...)

	var toolCalls []store.ToolCall
	toolErrorCount := 0

	for tIdx, tool := range rawTools {
		toolName := tool.Name
		if toolName == "" {
			toolName = tool.ToolName
		}
		if toolName == "" {
			continue
		}

		callID := tool.ID
		if callID == "" {
			callID = fmt.Sprintf("%s:tool:%d", reqID, tIdx)
		}

		tStatus := "success"
		stLower := strings.ToLower(tool.Status)
		if stLower == "error" || stLower == "failed" || tool.IsError || tool.Error != "" {
			tStatus = "error"
			toolErrorCount++
		}

		toolCalls = append(toolCalls, store.ToolCall{
			ID:        callID,
			RequestID: reqID,
			SessionID: sessionID,
			ToolName:  toolName,
			Status:    tStatus,
			CreatedAt: ts,
		})
	}

	return store.Request{
		ID:             reqID,
		Tool:           "windsurf",
		SessionID:      sessionID,
		Timestamp:      ts,
		Model:          model,
		InputTokens:    inTokens,
		OutputTokens:   outTokens,
		TotalTokens:    inTokens + outTokens,
		CostUSD:        costUSD,
		LatencyMS:      latencyMS,
		Status:         status,
		PromptText:     prompt,
		ResponseText:   resp,
		SourceFile:     sourcePath,
		StepIndex:      stepIdx,
		ToolCallCount:  len(toolCalls),
		ToolErrorCount: toolErrorCount,
		ToolCalls:      toolCalls,
		ImportedAt:     time.Now().UTC(),
	}
}
