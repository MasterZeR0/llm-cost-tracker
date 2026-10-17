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

type AGYParser struct{}

func init() {
	Register(&AGYParser{})
}

func (p *AGYParser) Name() string {
	return "agy"
}

func (p *AGYParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if filepath.Base(path) == "transcript.jsonl" {
				sources = append(sources, DataSource{Path: path, Type: "jsonl"})
			}
			return nil
		})
	}
	return sources, nil
}

type agyTranscriptEvent struct {
	StepIndex       *int64 `json:"step_index,omitempty"`
	Source          string `json:"source"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	Model           string `json:"model,omitempty"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
}

func (p *AGYParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
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
	var prevTs time.Time
	sameTsCount := int64(0)

	// Extract conversation id from path: .../brain/<id>/.system_generated/logs/transcript.jsonl
	parts := strings.Split(source.Path, string(filepath.Separator))
	sessionID := "agy-session"
	for i, part := range parts {
		if part == "brain" && i+1 < len(parts) {
			sessionID = parts[i+1]
			break
		}
	}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err == io.EOF && line[len(line)-1] != '\n' {
				// Incomplete trailing line; don't consume or advance offset past it yet
				break
			}
			currentOffset += int64(len(line))
			var evt agyTranscriptEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Type == "PLANNER_RESPONSE" {
				ts := time.Now().UTC()
				if evt.CreatedAt != "" {
					if parsed, tErr := time.Parse(time.RFC3339Nano, evt.CreatedAt); tErr == nil {
						ts = parsed
					} else if parsed, tErr := time.Parse(time.RFC3339, evt.CreatedAt); tErr == nil {
						ts = parsed
					}
				}
				// Use step_index if provided, or add offset on same-timestamp collision
				if evt.StepIndex != nil {
					ts = ts.Add(time.Duration(*evt.StepIndex) * time.Millisecond)
				} else if ts.Equal(prevTs) {
					sameTsCount++
					ts = ts.Add(time.Duration(sameTsCount) * time.Millisecond)
				} else {
					prevTs = ts
					sameTsCount = 0
				}

				model := "gemini-2-pro"
				if evt.Model != "" {
					model = evt.Model
				}
				req := store.Request{
					ID:              uuid.New().String(),
					Tool:            "agy",
					SessionID:       sessionID,
					Timestamp:       ts,
					Model:           model,
					InputTokens:     evt.InputTokens,
					OutputTokens:    evt.OutputTokens,
					CacheReadTokens: evt.CacheReadTokens,
					TotalTokens:     evt.InputTokens + evt.OutputTokens,
					Status:          "success",
					SourceFile:      source.Path,
					RawData:         strings.TrimRight(string(line), "\r\n"),
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
