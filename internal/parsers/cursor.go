package parsers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/tokenestimator"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/vscdb"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type CursorParser struct{}

func init() {
	Register(&CursorParser{})
}

func (p *CursorParser) Name() string {
	return "cursor"
}

func hasCursorData(path string) bool {
	r, err := vscdb.Open(path)
	if err != nil {
		return false
	}
	defer r.Close()

	val, err := r.GetKey("composer.composerData")
	if err == nil && len(val) > 0 {
		return true
	}
	val, err = r.GetKey("workbench.panel.aichat.chatdata")
	if err == nil && len(val) > 0 {
		return true
	}
	return false
}

func (p *CursorParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	searchPaths := cfg.Paths
	if len(searchPaths) == 0 {
		searchPaths = config.DefaultPathsForTool("cursor")
	}

	var sources []DataSource
	seen := make(map[string]bool)

	for _, rawPath := range searchPaths {
		expanded := config.ExpandHome(rawPath)
		fi, err := os.Stat(expanded)
		if err != nil {
			continue
		}

		if !fi.IsDir() {
			if strings.HasSuffix(expanded, ".vscdb") || filepath.Base(expanded) == "state.vscdb" {
				if !seen[expanded] && hasCursorData(expanded) {
					seen[expanded] = true
					sources = append(sources, DataSource{Path: expanded, Type: "sqlite"})
				}
			}
			continue
		}

		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if filepath.Base(path) == "state.vscdb" || strings.HasSuffix(path, ".vscdb") {
				if !seen[path] && hasCursorData(path) {
					seen[path] = true
					sources = append(sources, DataSource{Path: path, Type: "sqlite"})
				}
			}
			return nil
		})
	}

	return sources, nil
}

type cursorComposerData struct {
	AllComposers []cursorComposer `json:"allComposers"`
	Composers    []cursorComposer `json:"composers"`
}

type cursorComposer struct {
	ComposerID    string         `json:"composerId"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	CreatedAt     any            `json:"createdAt"`
	LastUpdatedAt any            `json:"lastUpdatedAt"`
	Conversation  []cursorBubble `json:"conversation"`
	Messages      []cursorBubble `json:"messages"`
	Bubbles       []cursorBubble `json:"bubbles"`
	ModelType     string         `json:"modelType"`
	Model         string         `json:"model"`
}

type cursorBubble struct {
	Type              string            `json:"type"`
	Role              string            `json:"role"`
	Text              string            `json:"text"`
	Content           string            `json:"content"`
	Body              string            `json:"body"`
	ModelType         string            `json:"modelType"`
	Model             string            `json:"model"`
	ModelName         string            `json:"modelName"`
	BubbleID          string            `json:"bubbleId"`
	ID                string            `json:"id"`
	Timestamp         any               `json:"timestamp"`
	CreatedAt         any               `json:"createdAt"`
	InputTokens       *int64            `json:"inputTokens"`
	OutputTokens      *int64            `json:"outputTokens"`
	InputTokensSnake  *int64            `json:"input_tokens"`
	OutputTokensSnake *int64            `json:"output_tokens"`
	Tokens            *int64            `json:"tokens"`
	TokenCount        *cursorTokenCount `json:"tokenCount"`
	Actions           []cursorAction    `json:"actions"`
	ToolCalls         []cursorAction    `json:"toolCalls"`
}

type cursorTokenCount struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}

type cursorAction struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	FilePath string `json:"filePath"`
	Status   string `json:"status"`
}

func isUserBubble(role, typ string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	t := strings.ToLower(strings.TrimSpace(typ))
	return r == "user" || t == "user" || t == "human" || t == "1"
}

func isAIBubble(role, typ string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	t := strings.ToLower(strings.TrimSpace(typ))
	return r == "ai" || r == "assistant" || t == "ai" || t == "assistant" || t == "bot" || t == "2"
}

func getBubbleText(b cursorBubble) string {
	if b.Text != "" {
		return b.Text
	}
	if b.Content != "" {
		return b.Content
	}
	if b.Body != "" {
		return b.Body
	}
	return ""
}

func getBubbleModel(b cursorBubble, fallback string) string {
	if b.ModelType != "" {
		return b.ModelType
	}
	if b.Model != "" {
		return b.Model
	}
	if b.ModelName != "" {
		return b.ModelName
	}
	return fallback
}

func normalizeModel(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return m
	}
	m = strings.ReplaceAll(m, "claude-3.5-", "claude-3-5-")
	m = strings.ReplaceAll(m, "claude-3.7-", "claude-3-7-")
	return m
}

func parseCursorTimestamp(val any) (time.Time, bool) {
	if val == nil {
		return time.Time{}, false
	}
	switch v := val.(type) {
	case int64:
		return parseCursorEpoch(v)
	case int:
		return parseCursorEpoch(int64(v))
	case float64:
		return parseCursorEpoch(int64(v))
	case time.Time:
		return v.UTC(), true
	case string:
		return parseCursorStringTime(v)
	default:
		return time.Time{}, false
	}
}

func parseCursorEpoch(epoch int64) (time.Time, bool) {
	if epoch <= 0 {
		return time.Time{}, false
	}
	if epoch > 100_000_000_000 {
		return time.UnixMilli(epoch).UTC(), true
	}
	return time.Unix(epoch, 0).UTC(), true
}

func parseCursorStringTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if num, err := strconv.ParseInt(s, 10, 64); err == nil {
		return parseCursorEpoch(num)
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (p *CursorParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	fi, err := os.Stat(source.Path)
	if err != nil {
		return nil, state, err
	}
	modTime := fi.ModTime().UTC()
	if state.LastModified != nil && (modTime.Before(*state.LastModified) || modTime.Equal(*state.LastModified)) {
		return nil, state, nil
	}

	reader, err := vscdb.Open(source.Path)
	if err != nil {
		// If ItemTable is missing or database is not a valid vscdb, fail safe without error
		return nil, state, nil
	}
	defer reader.Close()

	var composers []cursorComposer

	// 1. Query composer.composerData
	val, err := reader.GetKey("composer.composerData")
	if err == nil && len(val) > 0 {
		var wrapper cursorComposerData
		if jsonErr := json.Unmarshal(val, &wrapper); jsonErr == nil {
			composers = append(composers, wrapper.AllComposers...)
			composers = append(composers, wrapper.Composers...)
		} else {
			var list []cursorComposer
			if jsonErr := json.Unmarshal(val, &list); jsonErr == nil {
				composers = append(composers, list...)
			}
		}
	}

	// 2. Query workbench.panel.aichat.chatdata
	chatVal, err := reader.GetKey("workbench.panel.aichat.chatdata")
	if err == nil && len(chatVal) > 0 {
		var chatWrapper struct {
			Tabs []struct {
				TabID        string         `json:"tabId"`
				ID           string         `json:"id"`
				Bubbles      []cursorBubble `json:"bubbles"`
				Messages     []cursorBubble `json:"messages"`
				Conversation []cursorBubble `json:"conversation"`
			} `json:"tabs"`
			Chats []struct {
				ID       string         `json:"id"`
				Messages []cursorBubble `json:"messages"`
				Bubbles  []cursorBubble `json:"bubbles"`
			} `json:"chats"`
		}
		if jsonErr := json.Unmarshal(chatVal, &chatWrapper); jsonErr == nil {
			for _, tab := range chatWrapper.Tabs {
				tabID := tab.TabID
				if tabID == "" {
					tabID = tab.ID
				}
				bubbles := tab.Bubbles
				if len(bubbles) == 0 {
					bubbles = tab.Messages
				}
				if len(bubbles) == 0 {
					bubbles = tab.Conversation
				}
				composers = append(composers, cursorComposer{
					ComposerID:   tabID,
					Conversation: bubbles,
				})
			}
			for _, chat := range chatWrapper.Chats {
				bubbles := chat.Messages
				if len(bubbles) == 0 {
					bubbles = chat.Bubbles
				}
				composers = append(composers, cursorComposer{
					ComposerID:   chat.ID,
					Conversation: bubbles,
				})
			}
		}
	}

	pricingEngine := pricing.NewEngine(nil)
	var requests []store.Request
	globalTurnIndex := int64(0)
	var lastID string

	for _, comp := range composers {
		composerID := comp.ComposerID
		if composerID == "" {
			composerID = comp.ID
		}
		if composerID == "" {
			composerID = "unknown"
		}

		bubbles := comp.Conversation
		if len(bubbles) == 0 {
			bubbles = comp.Messages
		}
		if len(bubbles) == 0 {
			bubbles = comp.Bubbles
		}

		fallbackModel := comp.ModelType
		if fallbackModel == "" {
			fallbackModel = comp.Model
		}

		compTime := time.Now().UTC()
		if pt, ok := parseCursorTimestamp(comp.CreatedAt); ok {
			compTime = pt
		} else if pt, ok := parseCursorTimestamp(comp.LastUpdatedAt); ok {
			compTime = pt
		}

		turnIdx := 0
		var userPrompt string
		var userTime *time.Time

		for _, b := range bubbles {
			if isUserBubble(b.Role, b.Type) {
				text := getBubbleText(b)
				if userPrompt == "" {
					userPrompt = text
				} else {
					userPrompt += "\n" + text
				}
				if pt, ok := parseCursorTimestamp(b.Timestamp); ok {
					userTime = &pt
				} else if pt, ok := parseCursorTimestamp(b.CreatedAt); ok {
					userTime = &pt
				}
				continue
			}

			if isAIBubble(b.Role, b.Type) {
				responseText := getBubbleText(b)
				model := normalizeModel(getBubbleModel(b, fallbackModel))

				// Check tokens
				inTokPtr := b.InputTokens
				if inTokPtr == nil {
					inTokPtr = b.InputTokensSnake
				}
				outTokPtr := b.OutputTokens
				if outTokPtr == nil {
					outTokPtr = b.OutputTokensSnake
				}

				var inTok, outTok int64
				status := "success"

				if inTokPtr != nil || outTokPtr != nil {
					if inTokPtr != nil {
						inTok = *inTokPtr
					}
					if outTokPtr != nil {
						outTok = *outTokPtr
					}
				} else if b.TokenCount != nil && (b.TokenCount.InputTokens > 0 || b.TokenCount.OutputTokens > 0) {
					inTok = b.TokenCount.InputTokens
					outTok = b.TokenCount.OutputTokens
				}

				if inTok == 0 && outTok == 0 {
					inTok, outTok = tokenestimator.EstimateTokens(userPrompt, responseText)
					status = "estimated"
				}

				// Calculate cost
				var costUSD *float64
				if model != "" {
					cost, ok := pricingEngine.CalculateCost(model, inTok, outTok, 0, 0)
					if ok && cost != nil {
						costUSD = cost
					} else {
						zero := 0.0
						costUSD = &zero
					}
				} else {
					zero := 0.0
					costUSD = &zero
				}

				// Determine timestamp
				ts := compTime
				if pt, ok := parseCursorTimestamp(b.Timestamp); ok {
					ts = pt
				} else if pt, ok := parseCursorTimestamp(b.CreatedAt); ok {
					ts = pt
				} else if userTime != nil {
					ts = *userTime
				}
				// Sub-millisecond offset to guarantee monotonic ordering
				ts = ts.Add(time.Duration(turnIdx) * time.Millisecond)

				// Deterministic turn ID
				reqID := fmt.Sprintf("cursor:%s:%d", composerID, turnIdx)

				// Tool calls & actions
				var toolCalls []store.ToolCall
				toolErrorCount := 0
				for _, act := range append(b.Actions, b.ToolCalls...) {
					actName := act.Type
					if actName == "" {
						actName = act.Name
					}
					if actName == "" {
						actName = "action"
					}
					actStatus := "success"
					if strings.EqualFold(act.Status, "error") || strings.EqualFold(act.Status, "failed") {
						actStatus = "error"
						toolErrorCount++
					}
					toolCalls = append(toolCalls, store.ToolCall{
						ID:        uuid.New().String(),
						RequestID: reqID,
						SessionID: composerID,
						ToolName:  actName,
						Status:    actStatus,
						CreatedAt: ts,
					})
				}

				req := store.Request{
					ID:             reqID,
					Tool:           "cursor",
					SessionID:      composerID,
					Timestamp:      ts,
					Model:          model,
					InputTokens:    inTok,
					OutputTokens:   outTok,
					TotalTokens:    inTok + outTok,
					CostUSD:        costUSD,
					Status:         status,
					PromptText:     userPrompt,
					ResponseText:   responseText,
					SourceFile:     source.Path,
					StepIndex:      int64(turnIdx),
					ToolCallCount:  len(toolCalls),
					ToolErrorCount: toolErrorCount,
					ToolCalls:      toolCalls,
				}

				globalTurnIndex++
				if globalTurnIndex > state.LastOffset {
					requests = append(requests, req)
					lastID = reqID
				}

				userPrompt = ""
				userTime = nil
				turnIdx++
			}
		}
	}

	state.LastOffset = globalTurnIndex
	if lastID != "" {
		state.LastRowID = lastID
	}
	state.LastModified = &modTime
	state.SourcePath = source.Path
	state.Tool = "cursor"
	state.UpdatedAt = time.Now().UTC()

	return requests, state, nil
}
