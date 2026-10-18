package parsers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/tokenestimator"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type ClineParser struct{}

func init() {
	Register(&ClineParser{})
}

func (p *ClineParser) Name() string {
	return "cline"
}

func (p *ClineParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	return discoverTaskConversationFiles(cfg)
}

func (p *ClineParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	return parseTaskConversationHistory(source, state, "cline")
}

// clineMessage models a conversation turn in api_conversation_history.json.
type clineMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Model     string          `json:"model,omitempty"`
	Ts        any             `json:"ts,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Usage     *clineUsage     `json:"usage,omitempty"`
	Cost      *float64        `json:"cost,omitempty"`
	CostUSD   *float64        `json:"costUSD,omitempty"`
}

type clineUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	PromptTokens             int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens         int64 `json:"completion_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheWriteTokens         int64 `json:"cache_write_tokens,omitempty"`
	CacheReadTokens          int64 `json:"cache_read_tokens,omitempty"`
	TotalTokens              int64 `json:"total_tokens,omitempty"`
}

type clineContentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	ID      string          `json:"id,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

var xmlToolRegex = regexp.MustCompile(`<\s*(execute_command|read_file|write_to_file|replace_in_file|browser_action|use_mcp_tool|access_mcp_resource|ask_followup_question|attempt_completion|switch_mode|new_task)\b`)

func discoverTaskConversationFiles(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	seen := make(map[string]bool)

	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		fi, err := os.Stat(expanded)
		if err != nil {
			continue
		}

		if !fi.IsDir() {
			if strings.HasSuffix(expanded, "api_conversation_history.json") {
				if !seen[expanded] {
					seen[expanded] = true
					sources = append(sources, DataSource{Path: expanded, Type: "json"})
				}
			}
			continue
		}

		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info == nil {
				return nil
			}
			if !info.IsDir() && info.Name() == "api_conversation_history.json" {
				if !seen[path] {
					seen[path] = true
					sources = append(sources, DataSource{Path: path, Type: "json"})
				}
			}
			return nil
		})
	}

	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Path < sources[j].Path
	})
	return sources, nil
}

func parseTaskConversationHistory(source DataSource, state store.SyncState, toolName string) ([]store.Request, store.SyncState, error) {
	fi, err := os.Stat(source.Path)
	if err != nil {
		return nil, state, err
	}
	fileSize := fi.Size()
	modTime := fi.ModTime().UTC()

	// Truncation or rotation detection: if file size shrank, reset offset to 0
	if state.LastOffset > 0 {
		if fileSize < state.LastOffset {
			state.LastOffset = 0
		} else if fileSize == state.LastOffset {
			// Unchanged, idempotent early return
			return nil, state, nil
		}
	}

	if fileSize == 0 {
		state.LastOffset = 0
		state.LastModified = &modTime
		return nil, state, nil
	}

	data, err := os.ReadFile(source.Path)
	if err != nil {
		return nil, state, err
	}

	var messages []clineMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		// Try wrapper structure if present
		var wrapper struct {
			Messages []clineMessage `json:"messages"`
		}
		if wrapErr := json.Unmarshal(data, &wrapper); wrapErr == nil && len(wrapper.Messages) > 0 {
			messages = wrapper.Messages
		} else {
			return nil, state, fmt.Errorf("unmarshal conversation history: %w", err)
		}
	}

	sessionID := filepath.Base(filepath.Dir(source.Path))
	if sessionID == "" || sessionID == "." || sessionID == "/" {
		sessionID = strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
	}

	engine := pricing.NewEngine(nil)
	var requests []store.Request
	var lastPromptText string
	var currentModel string
	var prevTs time.Time
	sameTsCount := int64(0)

	for _, msg := range messages {
		if msg.Model != "" {
			currentModel = msg.Model
		}

		if msg.Role == "user" {
			userText, isErr := extractUserTextAndError(msg.Content)
			lastPromptText = userText

			// If error was detected in user tool result, associate with previous request
			if isErr && len(requests) > 0 {
				lastReq := &requests[len(requests)-1]
				marked := false
				for i := len(lastReq.ToolCalls) - 1; i >= 0; i-- {
					if lastReq.ToolCalls[i].Status != "error" {
						lastReq.ToolCalls[i].Status = "error"
						lastReq.ToolErrorCount++
						marked = true
						break
					}
				}
				if !marked {
					lastReq.ToolErrorCount++
				}
			}
			continue
		}

		if msg.Role != "assistant" {
			continue
		}

		stepIndex := int64(len(requests))
		reqID := fmt.Sprintf("%s:%s:%d", toolName, sessionID, stepIndex)

		// Parse timestamp
		ts := parseMessageTime(msg.Ts, msg.Timestamp, modTime.Add(time.Duration(stepIndex)*time.Second))
		if ts.Equal(prevTs) {
			sameTsCount++
			ts = ts.Add(time.Duration(sameTsCount) * time.Microsecond)
		} else {
			prevTs = ts
			sameTsCount = 0
		}

		// Extract text content and tool calls
		responseText, toolCalls := extractAssistantContentAndTools(msg.Content, reqID, sessionID, ts)

		// Extract token usage
		var inTokens, outTokens, cacheReadTokens, cacheWriteTokens, totalTokens int64
		if msg.Usage != nil {
			inTokens = msg.Usage.InputTokens
			if inTokens == 0 {
				inTokens = msg.Usage.PromptTokens
			}
			outTokens = msg.Usage.OutputTokens
			if outTokens == 0 {
				outTokens = msg.Usage.CompletionTokens
			}
			cacheReadTokens = msg.Usage.CacheReadInputTokens
			if cacheReadTokens == 0 {
				cacheReadTokens = msg.Usage.CacheReadTokens
			}
			cacheWriteTokens = msg.Usage.CacheCreationInputTokens
			if cacheWriteTokens == 0 {
				cacheWriteTokens = msg.Usage.CacheWriteTokens
			}
			totalTokens = msg.Usage.TotalTokens
			if totalTokens == 0 {
				totalTokens = inTokens + outTokens
			}
		}

		model := currentModel
		if model == "" {
			model = "claude-3-5-sonnet-20241022"
		}

		status := "success"
		if inTokens == 0 && outTokens == 0 {
			estIn, estOut := tokenestimator.EstimateTokens(lastPromptText, responseText)
			inTokens = estIn
			outTokens = estOut
			totalTokens = inTokens + outTokens
			status = "estimated"
		}

		var costUSD *float64
		if msg.CostUSD != nil {
			costUSD = msg.CostUSD
		} else if msg.Cost != nil {
			costUSD = msg.Cost
		} else {
			if cost, ok := engine.CalculateCost(model, inTokens, outTokens, cacheReadTokens, cacheWriteTokens); ok {
				costUSD = cost
			} else {
				zero := 0.0
				costUSD = &zero
			}
		}

		rawBytes, _ := json.Marshal(msg)

		req := store.Request{
			ID:               reqID,
			Tool:             toolName,
			SessionID:        sessionID,
			Timestamp:        ts,
			Model:            model,
			InputTokens:      inTokens,
			OutputTokens:     outTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
			TotalTokens:      totalTokens,
			CostUSD:          costUSD,
			Status:           status,
			PromptText:       lastPromptText,
			ResponseText:     responseText,
			SourceFile:       source.Path,
			RawData:          string(rawBytes),
			StepIndex:        stepIndex,
			ToolCallCount:    len(toolCalls),
			ToolCalls:        toolCalls,
		}

		requests = append(requests, req)
	}

	state.LastOffset = fileSize
	state.LastModified = &modTime
	state.SourcePath = source.Path
	state.Tool = toolName

	return requests, state, nil
}

func extractAssistantContentAndTools(contentRaw json.RawMessage, reqID, sessionID string, ts time.Time) (string, []store.ToolCall) {
	var toolCalls []store.ToolCall
	var textParts []string

	if len(contentRaw) == 0 {
		return "", toolCalls
	}

	// Case 1: Simple string
	var simpleText string
	if err := json.Unmarshal(contentRaw, &simpleText); err == nil {
		textParts = append(textParts, simpleText)
	} else {
		// Case 2: Array of blocks
		var blocks []clineContentBlock
		if err := json.Unmarshal(contentRaw, &blocks); err == nil {
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					textParts = append(textParts, b.Text)
				} else if b.Type == "tool_use" {
					toolName := b.Name
					if toolName == "" {
						toolName = "tool_use"
					}
					toolCalls = append(toolCalls, store.ToolCall{
						ID:        uuid.New().String(),
						RequestID: reqID,
						SessionID: sessionID,
						ToolName:  toolName,
						Status:    "success",
						CreatedAt: ts,
					})
				}
			}
		}
	}

	fullText := strings.Join(textParts, "\n")

	// Detect tool XML blocks: <execute_command>...</execute_command>, etc.
	matches := xmlToolRegex.FindAllStringSubmatch(fullText, -1)
	for _, m := range matches {
		if len(m) > 1 {
			toolName := m[1]
			toolCalls = append(toolCalls, store.ToolCall{
				ID:        uuid.New().String(),
				RequestID: reqID,
				SessionID: sessionID,
				ToolName:  toolName,
				Status:    "success",
				CreatedAt: ts,
			})
		}
	}

	return fullText, toolCalls
}

func extractUserTextAndError(contentRaw json.RawMessage) (string, bool) {
	if len(contentRaw) == 0 {
		return "", false
	}

	var simpleText string
	if err := json.Unmarshal(contentRaw, &simpleText); err == nil {
		return simpleText, checkErrorInText(simpleText)
	}

	var blocks []clineContentBlock
	if err := json.Unmarshal(contentRaw, &blocks); err == nil {
		var texts []string
		hasErr := false
		for _, b := range blocks {
			if b.IsError {
				hasErr = true
			}
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
			if len(b.Content) > 0 {
				var innerStr string
				if json.Unmarshal(b.Content, &innerStr) == nil {
					texts = append(texts, innerStr)
				}
			}
		}
		combined := strings.Join(texts, "\n")
		return combined, hasErr || checkErrorInText(combined)
	}

	return string(contentRaw), checkErrorInText(string(contentRaw))
}

func checkErrorInText(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "error:") ||
		strings.Contains(lower, "[error]") ||
		strings.Contains(lower, "exit code 1") ||
		strings.Contains(lower, "command failed") ||
		strings.Contains(lower, "tool failed") ||
		strings.Contains(lower, "failed with exit code") ||
		strings.Contains(lower, "error executing") {
		return true
	}
	return false
}

func parseMessageTime(tsVal any, tsStr string, fallback time.Time) time.Time {
	if tsStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			return t.UTC()
		}
	}
	if tsVal != nil {
		switch v := tsVal.(type) {
		case float64:
			if v > 100_000_000_000 {
				return time.UnixMilli(int64(v)).UTC()
			} else if v > 0 {
				return time.Unix(int64(v), 0).UTC()
			}
		case int64:
			if v > 100_000_000_000 {
				return time.UnixMilli(v).UTC()
			} else if v > 0 {
				return time.Unix(v, 0).UTC()
			}
		case int:
			if int64(v) > 100_000_000_000 {
				return time.UnixMilli(int64(v)).UTC()
			} else if v > 0 {
				return time.Unix(int64(v), 0).UTC()
			}
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t.UTC()
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t.UTC()
			}
		}
	}
	return fallback
}
