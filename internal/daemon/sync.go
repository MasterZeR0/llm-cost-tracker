package daemon

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

// SyncAll iterates through all configured and enabled tools, discovers data sources,
// incrementally parses new log/database records, calculates missing token costs via
// the pricing engine, and inserts unique requests into the persistent store.
func SyncAll(cfg *config.Config, st *store.Store, engine *pricing.Engine) (int, error) {
	if cfg == nil || st == nil {
		return 0, nil
	}

	totalImported := 0

	for toolName, toolCfg := range cfg.Tools {
		if !toolCfg.Enabled {
			continue
		}

		p, ok := parsers.Get(toolName)
		if !ok {
			slog.Debug("parser not found for tool", "tool", toolName)
			continue
		}

		sources, err := p.Discover(toolCfg)
		if err != nil {
			slog.Warn("failed to discover sources for tool", "tool", toolName, "err", err)
			continue
		}

		for _, src := range sources {
			state, err := st.GetSyncState(src.Path)
			if err != nil {
				slog.Warn("failed to get sync state", "file", src.Path, "err", err)
				continue
			}
			state.Tool = toolName

			reqs, newState, err := p.Parse(src, *state)
			if err != nil {
				slog.Warn("failed to parse source", "file", src.Path, "err", err)
				continue
			}

			// If tool did not supply cost, calculate it using pricing engine
			if engine != nil {
				for i := range reqs {
					if reqs[i].CostUSD == nil {
						cost, ok := engine.CalculateCost(
							reqs[i].Model,
							reqs[i].InputTokens,
							reqs[i].OutputTokens,
							reqs[i].CacheReadTokens,
							reqs[i].CacheWriteTokens,
						)
						if ok {
							reqs[i].CostUSD = cost
						}
					}
				}
			}

			inserted, err := st.InsertRequests(reqs)
			if err != nil {
				slog.Error("failed to insert requests", "file", src.Path, "err", err)
				continue
			}
			totalImported += inserted

			if newState.SourcePath == "" {
				newState.SourcePath = src.Path
			}
			if newState.Tool == "" {
				newState.Tool = toolName
			}

			if err := st.SetSyncState(newState); err != nil {
				slog.Warn("failed to update sync state", "file", src.Path, "err", err)
			}
		}
	}

	if agyCfg, ok := cfg.Tools["agy"]; ok && agyCfg.Enabled && len(agyCfg.Paths) > 0 {
		brainPath := config.ExpandHome(agyCfg.Paths[0])
		convDir := filepath.Join(filepath.Dir(brainPath), "conversations")
		if _, statErr := os.Stat(convDir); statErr == nil {
			_, _ = parsers.BackfillAGYLatencies(st.DB(), convDir)
		}
	}

	return totalImported, nil
}
