package parsers

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
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

type agyToolCall struct {
	Name string `json:"name"`
}

type agyTranscriptEvent struct {
	StepIndex       *int64        `json:"step_index,omitempty"`
	Source          string        `json:"source"`
	Type            string        `json:"type"`
	Status          string        `json:"status"`
	CreatedAt       string        `json:"created_at"`
	Model           string        `json:"model,omitempty"`
	Content         string        `json:"content,omitempty"`
	InputTokens     int64         `json:"input_tokens"`
	OutputTokens    int64         `json:"output_tokens"`
	CacheReadTokens int64         `json:"cache_read_tokens"`
	ToolCalls       []agyToolCall `json:"tool_calls,omitempty"`
	ExitCode        *int          `json:"exit_code,omitempty"`
}


type agyStepTokens struct {
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
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

	sessionModels, sessionStepTokens, fallbackModel := loadAGYSessionData(source.Path, sessionID)
	turnCount := countPlannerResponsesBefore(source.Path, state.LastOffset)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err == io.EOF && line[len(line)-1] != '\n' {
				// Incomplete trailing line; don't consume or advance offset past it yet
				break
			}
			currentOffset += int64(len(line))
			var evt agyTranscriptEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil {
				// Track runtime model change notifications in user input
				if evt.Type == "USER_INPUT" && strings.Contains(evt.Content, "Model Selection") {
					if dynamicModel := extractModelFromSettingsChange(evt.Content); dynamicModel != "" {
						fallbackModel = dynamicModel
					}
				}

				if evt.Type == "PLANNER_RESPONSE" {
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

					model := fallbackModel
					if m, ok := sessionModels[turnCount]; ok && m != "" {
						model = m
						fallbackModel = m
					}
					if evt.Model != "" {
						model = normalizeAGYModel(evt.Model)
					}
					if model == "" {
						model = "gemini-2-pro"
					}
					turnCount++

					inTokens := evt.InputTokens
					outTokens := evt.OutputTokens
					cacheTokens := evt.CacheReadTokens

					// Backfill token counts from conversation DB steps table if missing from transcript (older AGY logs)
					if inTokens == 0 && outTokens == 0 && evt.StepIndex != nil {
						if st, ok := sessionStepTokens[*evt.StepIndex]; ok {
							inTokens = st.InputTokens
							outTokens = st.OutputTokens
							cacheTokens = st.CacheReadTokens
						}
					}

					reqID := uuid.New().String()
					var toolCalls []store.ToolCall
					var subagentCalls int
					for _, tc := range evt.ToolCalls {
						toolCalls = append(toolCalls, store.ToolCall{
							ID:        uuid.New().String(),
							RequestID: reqID,
							SessionID: sessionID,
							ToolName:  tc.Name,
							Status:    "success",
							CreatedAt: ts,
						})
						if tc.Name == "invoke_subagent" || tc.Name == "manage_subagents" {
							subagentCalls++
						}
					}

					stepIdx := int64(0)
					if evt.StepIndex != nil {
						stepIdx = *evt.StepIndex
					}

					req := store.Request{
						ID:                reqID,
						Tool:              "agy",
						SessionID:         sessionID,
						Timestamp:         ts,
						Model:             model,
						InputTokens:       inTokens,
						OutputTokens:      outTokens,
						CacheReadTokens:   cacheTokens,
						TotalTokens:       inTokens + outTokens,
						Status:            "success",
						SourceFile:        source.Path,
						RawData:           strings.TrimRight(string(line), "\r\n"),
						StepIndex:         stepIdx,
						ToolCallCount:     len(toolCalls),
						SubagentCallCount: subagentCalls,
						ToolCalls:         toolCalls,
					}
					requests = append(requests, req)
				} else if len(requests) > 0 {
					// Correlate execution events with preceding request's tool calls
					isErr := evt.Status == "ERROR" || (evt.ExitCode != nil && *evt.ExitCode != 0)
					if isErr {
						lastReq := &requests[len(requests)-1]
						targetTool := strings.ToLower(evt.Type)
						marked := false
						// 1. First try matching by tool name
						for i := range lastReq.ToolCalls {
							if strings.ToLower(lastReq.ToolCalls[i].ToolName) == targetTool && lastReq.ToolCalls[i].Status != "error" {
								lastReq.ToolCalls[i].Status = "error"
								lastReq.ToolErrorCount++
								marked = true
								break
							}
						}
						// 2. Fallback to first non-error tool call
						if !marked {
							for i := range lastReq.ToolCalls {
								if lastReq.ToolCalls[i].Status != "error" {
									lastReq.ToolCalls[i].Status = "error"
									lastReq.ToolErrorCount++
									marked = true
									break
								}
							}
						}
						if !marked {
							lastReq.ToolErrorCount++
						}
					}
				}

			}
		}

		if err != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	return requests, state, nil
}

func countPlannerResponsesBefore(filePath string, offset int64) int {
	if offset <= 0 {
		return 0
	}
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()

	buf := make([]byte, 64*1024)
	var totalRead int64
	var remainder []byte
	target := []byte(`"type":"PLANNER_RESPONSE"`)
	count := 0

	for totalRead < offset {
		toRead := int64(len(buf))
		if offset-totalRead < toRead {
			toRead = offset - totalRead
		}
		n, rErr := file.Read(buf[:toRead])
		if n > 0 {
			chunk := append(remainder, buf[:n]...)
			count += bytes.Count(chunk, target)
			if len(chunk) >= len(target) {
				remainder = chunk[len(chunk)-len(target)+1:]
			} else {
				remainder = chunk
			}
			totalRead += int64(n)
		}
		if rErr != nil {
			break
		}
	}
	return count
}

func loadAGYSessionData(sourcePath string, sessionID string) (map[int]string, map[int64]agyStepTokens, string) {
	sessionModels := make(map[int]string)
	sessionStepTokens := make(map[int64]agyStepTokens)
	fallbackModel := ""

	brainMarker := string(filepath.Separator) + "brain" + string(filepath.Separator)
	idx := strings.Index(sourcePath, brainMarker)
	if idx == -1 {
		return sessionModels, sessionStepTokens, fallbackModel
	}
	agyDir := sourcePath[:idx]

	// 1. Try reading global settings.json
	settingsPath := filepath.Join(agyDir, "settings.json")
	if data, err := os.ReadFile(settingsPath); err == nil {
		var s struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(data, &s) == nil && s.Model != "" {
			fallbackModel = normalizeAGYModel(s.Model)
		}
	}

	// 2. Try reading conversation SQLite database: conversations/<sessionID>.db
	dbPath := filepath.Join(agyDir, "conversations", sessionID+".db")
	if _, err := os.Stat(dbPath); err == nil {
		db, err := sql.Open("sqlite3", dbPath+"?mode=ro&_busy_timeout=1000")
		if err == nil {
			defer db.Close()

			// Load generation model metadata
			rows, qErr := db.Query("SELECT idx, data FROM gen_metadata ORDER BY idx ASC")
			if qErr == nil {
				for rows.Next() {
					var gIdx int
					var data []byte
					if err := rows.Scan(&gIdx, &data); err == nil && len(data) > 0 {
						if rawModel := extractModelFromProtobuf(data); rawModel != "" {
							normalized := normalizeAGYModel(rawModel)
							sessionModels[gIdx] = normalized
							fallbackModel = normalized
						}
					}
				}
				rows.Close()
			}

			// Load step token metadata from steps table
			sRows, sErr := db.Query("SELECT idx, metadata FROM steps WHERE metadata IS NOT NULL")
			if sErr == nil {
				for sRows.Next() {
					var stepIdx int64
					var meta []byte
					if err := sRows.Scan(&stepIdx, &meta); err == nil && len(meta) > 0 {
						inT, outT, cacheT := extractTokensFromStepMetadata(meta)
						if inT > 0 || outT > 0 {
							sessionStepTokens[stepIdx] = agyStepTokens{
								InputTokens:     inT,
								OutputTokens:    outT,
								CacheReadTokens: cacheT,
							}
						}
					}
				}
				sRows.Close()
			}
		}
	}

	return sessionModels, sessionStepTokens, fallbackModel
}

func extractTokensFromStepMetadata(meta []byte) (int64, int64, int64) {
	for i := 0; i < len(meta)-2; i++ {
		// Field 9 (wire type 2): (9 << 3) | 2 = 74 = 0x4a
		if meta[i] == 0x4a {
			l, n := binary.Uvarint(meta[i+1:])
			if n > 0 && l > 0 && l < 2000 && i+1+n+int(l) <= len(meta) {
				sub := meta[i+1+n : i+1+n+int(l)]
				inT, outT, cacheT := parseTokenSubmessage(sub)
				if inT > 0 || outT > 0 {
					return inT, outT, cacheT
				}
			}
		}
	}
	return 0, 0, 0
}

func parseTokenSubmessage(buf []byte) (int64, int64, int64) {
	var inT, outT, cacheT int64
	p := 0
	for p < len(buf) {
		tag, tn := binary.Uvarint(buf[p:])
		if tn <= 0 {
			break
		}
		p += tn
		wire := tag & 7
		fnum := tag >> 3
		switch wire {
		case 0: // varint
			val, vn := binary.Uvarint(buf[p:])
			if vn <= 0 {
				return inT, outT, cacheT
			}
			p += vn
			switch fnum {
			case 2:
				inT = int64(val)
			case 3:
				outT = int64(val)
			case 5:
				cacheT = int64(val)
			}
		case 2: // length-delimited
			l, ln := binary.Uvarint(buf[p:])
			if ln <= 0 {
				return inT, outT, cacheT
			}
			p += ln + int(l)
		default:
			return inT, outT, cacheT
		}
	}
	return inT, outT, cacheT
}

func extractModelFromProtobuf(data []byte) string {
	for i := 0; i < len(data)-2; i++ {
		// Field 19 (wire type 2): (19 << 3) | 2 = 154 = 0x9a, followed by varint 1 = 0x01
		if data[i] == 0x9a && data[i+1] == 0x01 {
			length, n := binary.Uvarint(data[i+2:])
			if n > 0 && length > 0 && length < 100 && i+2+n+int(length) <= len(data) {
				candidate := string(data[i+2+n : i+2+n+int(length)])
				if isRecognizedModel(candidate) {
					return candidate
				}
			}
		}
	}
	return ""
}

func isRecognizedModel(s string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range []string{"gemini", "claude", "gpt", "o1", "o3", "deepseek"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func extractModelFromSettingsChange(content string) string {
	idx := strings.Index(content, "<USER_SETTINGS_CHANGE>")
	if idx == -1 {
		return ""
	}
	endIdx := strings.Index(content, "</USER_SETTINGS_CHANGE>")
	if endIdx == -1 {
		endIdx = len(content)
	}
	block := content[idx:endIdx]
	marker := "Model Selection` from"
	mIdx := strings.Index(block, marker)
	if mIdx == -1 {
		return ""
	}
	toMarker := "to "
	tIdx := strings.Index(block[mIdx:], toMarker)
	if tIdx == -1 {
		return ""
	}
	rest := block[mIdx+tIdx+len(toMarker):]
	if dotIdx := strings.Index(rest, ". "); dotIdx != -1 {
		rest = rest[:dotIdx]
	} else if dotIdx := strings.Index(rest, ".\n"); dotIdx != -1 {
		rest = rest[:dotIdx]
	} else if dotIdx := strings.Index(rest, ".\r"); dotIdx != -1 {
		rest = rest[:dotIdx]
	} else if nlIdx := strings.Index(rest, "\n"); nlIdx != -1 {
		rest = rest[:nlIdx]
	}
	return normalizeAGYModel(rest)
}

func normalizeAGYModel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)

	for _, note := range []string{" (medium)", " (thinking)", " (high)", "(medium)", "(thinking)", "(high)"} {
		lower = strings.ReplaceAll(lower, note, "")
	}
	lower = strings.TrimSpace(lower)

	if lower == "gemini-pro-default" {
		return "gemini-2-pro"
	}

	lower = strings.TrimSuffix(lower, "-n")
	lower = strings.TrimSuffix(lower, "-tiered")
	lower = strings.ReplaceAll(lower, " ", "-")

	if strings.HasPrefix(lower, "gemini-3.8") {
		lower = "gemini-3-8" + strings.TrimPrefix(lower, "gemini-3.8")
	} else if strings.HasPrefix(lower, "gemini-3.1") {
		lower = "gemini-3-1" + strings.TrimPrefix(lower, "gemini-3.1")
	}

	if strings.HasPrefix(lower, "claude-opus-4.") {
		lower = "claude-opus-4-" + strings.TrimPrefix(lower, "claude-opus-4.")
	}

	return lower
}
