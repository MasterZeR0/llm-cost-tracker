package parsers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/tokenestimator"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type CopilotCLIParser struct{}

func init() {
	Register(&CopilotCLIParser{})
}

func (p *CopilotCLIParser) Name() string {
	return "copilot_cli"
}

func (p *CopilotCLIParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasSuffix(base, ".jsonl") {
				sources = append(sources, DataSource{Path: path, Type: "jsonl"})
			} else if strings.HasSuffix(base, ".json") {
				sources = append(sources, DataSource{Path: path, Type: "json"})
			}
			return nil
		})
	}
	return sources, nil
}

type copilotCLIEntry struct {
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id"`
	Timestamp   string   `json:"timestamp"`
	CommandType string   `json:"command_type"`
	Type        string   `json:"type"`
	Query       string   `json:"query"`
	Prompt      string   `json:"prompt"`
	Explanation string   `json:"explanation"`
	Suggestion  string   `json:"suggestion"`
	Response    string   `json:"response"`
	Command     string   `json:"command"`
	Model       string   `json:"model"`
	LatencyMS   *int64   `json:"latency_ms"`
	DurationMS  *int64   `json:"duration_ms"`
	CostUSD     *float64 `json:"cost_usd"`
}

func (p *CopilotCLIParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, state, err
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return nil, state, err
	}

	// Truncation detection: if file shrank below LastOffset, reset offset
	if state.LastOffset > 0 && fi.Size() < state.LastOffset {
		state.LastOffset = 0
		state.LastRowID = ""
	}

	if state.LastOffset > 0 {
		if _, seekErr := file.Seek(state.LastOffset, io.SeekStart); seekErr != nil {
			state.LastOffset = 0
			state.LastRowID = ""
			_, _ = file.Seek(0, io.SeekStart)
		}
	}

	lastStepIndex := int64(0)
	if state.LastOffset > 0 && state.LastRowID != "" {
		lastStepIndex, _ = strconv.ParseInt(state.LastRowID, 10, 64)
	}

	reader := bufio.NewReader(file)
	var requests []store.Request
	currentOffset := state.LastOffset
	pricingEngine := pricing.NewEngine(nil)

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if readErr == io.EOF && line[len(line)-1] != '\n' {
				// Trailing line is incomplete; do not consume offset past it
				break
			}
			currentOffset += int64(len(line))

			trimmed := strings.TrimSpace(string(line))
			if len(trimmed) == 0 {
				if readErr != nil {
					break
				}
				continue
			}

			var entry copilotCLIEntry
			if err := json.Unmarshal([]byte(trimmed), &entry); err != nil {
				if readErr != nil {
					break
				}
				continue
			}

			lastStepIndex++
			stepIdx := lastStepIndex

			sessionID := entry.SessionID
			if sessionID == "" {
				sessionID = strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
			}

			model := entry.Model
			if model == "" {
				model = "gpt-4o"
			}

			prompt := entry.Query
			if prompt == "" {
				prompt = entry.Prompt
			}

			resp := entry.Explanation
			if entry.Suggestion != "" {
				if resp != "" {
					resp = fmt.Sprintf("%s\n\nSuggested command: %s", resp, entry.Suggestion)
				} else {
					resp = entry.Suggestion
				}
			} else if entry.Command != "" {
				if resp != "" {
					resp = fmt.Sprintf("%s\n\nCommand: %s", resp, entry.Command)
				} else {
					resp = entry.Command
				}
			} else if entry.Response != "" {
				if resp == "" {
					resp = entry.Response
				}
			}

			inTokens, outTokens := tokenestimator.EstimateTokens(prompt, resp)

			var costUSD *float64
			if entry.CostUSD != nil {
				costUSD = entry.CostUSD
			} else {
				cost, ok := pricingEngine.CalculateCost(model, inTokens, outTokens, 0, 0)
				if ok {
					costUSD = cost
				} else {
					zero := 0.0
					costUSD = &zero
				}
			}

			var latencyMS *int64
			if entry.DurationMS != nil {
				latencyMS = entry.DurationMS
			} else if entry.LatencyMS != nil {
				latencyMS = entry.LatencyMS
			}

			ts := time.Now().UTC()
			if entry.Timestamp != "" {
				if parsed, parseErr := time.Parse(time.RFC3339Nano, entry.Timestamp); parseErr == nil {
					ts = parsed.UTC()
				} else if parsed, parseErr := time.Parse(time.RFC3339, entry.Timestamp); parseErr == nil {
					ts = parsed.UTC()
				}
			}

			reqID := fmt.Sprintf("copilot_cli:%s:%d", sessionID, stepIdx)

			var toolCalls []store.ToolCall
			if entry.CommandType == "suggest" || entry.Suggestion != "" {
				toolCalls = append(toolCalls, store.ToolCall{
					ID:        fmt.Sprintf("%s:tool:0", reqID),
					RequestID: reqID,
					SessionID: sessionID,
					ToolName:  "suggest_command",
					Status:    "success",
					CreatedAt: ts,
				})
			}

			req := store.Request{
				ID:            reqID,
				Tool:          "copilot_cli",
				SessionID:     sessionID,
				Timestamp:     ts,
				Model:         model,
				InputTokens:   inTokens,
				OutputTokens:  outTokens,
				TotalTokens:   inTokens + outTokens,
				CostUSD:       costUSD,
				LatencyMS:     latencyMS,
				Status:        "estimated",
				PromptText:    prompt,
				ResponseText:  resp,
				SourceFile:    source.Path,
				StepIndex:     stepIdx,
				ToolCallCount: len(toolCalls),
				ToolCalls:     toolCalls,
				ImportedAt:    time.Now().UTC(),
			}

			requests = append(requests, req)
		}

		if readErr != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	state.LastRowID = strconv.FormatInt(lastStepIndex, 10)
	modTime := fi.ModTime()
	state.LastModified = &modTime

	return requests, state, nil
}
