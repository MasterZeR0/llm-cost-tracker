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

type AiderParser struct{}

func init() {
	Register(&AiderParser{})
}

func (p *AiderParser) Name() string {
	return "aider"
}

func (p *AiderParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	if cfg.AnalyticsLog != "" {
		expanded := config.ExpandHome(cfg.AnalyticsLog)
		if _, err := os.Stat(expanded); err == nil {
			sources = append(sources, DataSource{Path: expanded, Type: "jsonl"})
		}
	}
	return sources, nil
}

type aiderAnalyticsEvent struct {
	Event      string `json:"event"`
	Properties struct {
		MainModel        string   `json:"main_model"`
		PromptTokens     int64    `json:"prompt_tokens"`
		CompletionTokens int64    `json:"completion_tokens"`
		TotalTokens      int64    `json:"total_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"properties"`
	Time int64 `json:"time"`
}

func (p *AiderParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
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

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err == io.EOF && line[len(line)-1] != '\n' {
				// Incomplete trailing line; don't consume or advance offset past it yet
				break
			}
			currentOffset += int64(len(line))
			var evt aiderAnalyticsEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Event == "message_send" {
				ts := time.Now().UTC()
				if evt.Time > 0 {
					ts = time.Unix(evt.Time, 0).UTC()
				}
				totalTokens := evt.Properties.TotalTokens
				if totalTokens == 0 {
					totalTokens = evt.Properties.PromptTokens + evt.Properties.CompletionTokens
				}
				req := store.Request{
					ID:           uuid.New().String(),
					Tool:         "aider",
					SessionID:    filepath.Base(source.Path),
					Timestamp:    ts,
					Model:        evt.Properties.MainModel,
					InputTokens:  evt.Properties.PromptTokens,
					OutputTokens: evt.Properties.CompletionTokens,
					TotalTokens:  totalTokens,
					CostUSD:      evt.Properties.Cost,
					Status:       "success",
					SourceFile:   source.Path,
					RawData:      strings.TrimRight(string(line), "\r\n"),
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
