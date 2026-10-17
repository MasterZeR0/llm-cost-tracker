package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type ClaudeCodeParser struct{}

func init() {
	Register(&ClaudeCodeParser{})
}

func (p *ClaudeCodeParser) Name() string {
	return "claude_code"
}

func (p *ClaudeCodeParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".jsonl") {
				sources = append(sources, DataSource{Path: path, Type: "jsonl"})
			}
			return nil
		})
	}
	return sources, nil
}

type claudeAssistantEvent struct {
	Type              string   `json:"type"`
	CostUSD           *float64 `json:"costUSD"`
	DurationMS        *int64   `json:"durationMs"`
	Timestamp         string   `json:"timestamp,omitempty"`
	Error             string   `json:"error,omitempty"`
	IsApiErrorMessage bool     `json:"isApiErrorMessage,omitempty"`
	Message           struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (p *ClaudeCodeParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, state, err
	}
	defer file.Close()

	if state.LastOffset > 0 {
		fi, statErr := file.Stat()
		if statErr == nil && fi.Size() < state.LastOffset {
			state.LastOffset = 0
			_, _ = file.Seek(0, io.SeekStart)
		} else if _, seekErr := file.Seek(state.LastOffset, io.SeekStart); seekErr != nil {
			state.LastOffset = 0
			_, _ = file.Seek(0, io.SeekStart)
		}
	}

	reader := bufio.NewReader(file)
	var requests []store.Request
	currentOffset := state.LastOffset
	sessionID := strings.TrimSuffix(filepath.Base(source.Path), ".jsonl")
	var prevTs time.Time
	sameTsCount := int64(0)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err == io.EOF && line[len(line)-1] != '\n' {
				// Incomplete trailing line; don't consume or advance offset past it yet
				break
			}
			currentOffset += int64(len(line))
			var evt claudeAssistantEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Type == "assistant" {
				ts := time.Now().UTC().Add(time.Duration(len(requests)) * time.Second)
				if evt.Timestamp != "" {
					if parsedTs, err := time.Parse(time.RFC3339Nano, evt.Timestamp); err == nil {
						ts = parsedTs
					} else if parsedTs, err := time.Parse(time.RFC3339, evt.Timestamp); err == nil {
						ts = parsedTs
					}
				}
				if ts.Equal(prevTs) {
					sameTsCount++
					ts = ts.Add(time.Duration(sameTsCount) * time.Microsecond)
				} else {
					prevTs = ts
					sameTsCount = 0
				}

				status := "success"
				if evt.Error != "" {
					status = "error"
				} else if evt.IsApiErrorMessage {
					status = "error"
				}

				req := store.Request{
					ID:               uuid.New().String(),
					Tool:             "claude_code",
					SessionID:        sessionID,
					Timestamp:        ts,
					Model:            evt.Message.Model,
					InputTokens:      evt.Message.Usage.InputTokens,
					OutputTokens:     evt.Message.Usage.OutputTokens,
					CacheReadTokens:  evt.Message.Usage.CacheReadInputTokens,
					CacheWriteTokens: evt.Message.Usage.CacheCreationInputTokens,
					TotalTokens:      evt.Message.Usage.InputTokens + evt.Message.Usage.OutputTokens,
					CostUSD:          evt.CostUSD,
					LatencyMS:        evt.DurationMS,
					Status:           status,
					SourceFile:       source.Path,
					RawData:          strings.TrimRight(string(line), "\r\n"),
				}
				requests = append(requests, req)
			}
		}
		if err != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	return requests, state, nil
}
