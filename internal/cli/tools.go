package cli

import (
	"fmt"
	"sort"

	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/spf13/cobra"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "List supported and discovered CLI tools",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Configured Tools:")
		names := make([]string, 0, len(cfg.Tools))
		for name := range cfg.Tools {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			toolCfg := cfg.Tools[name]
			status := "disabled"
			if toolCfg.Enabled {
				status = "enabled"
			}
			p, registered := parsers.Get(name)
			sourceCount := 0
			if registered {
				sources, _ := p.Discover(toolCfg)
				sourceCount = len(sources)
			}
			fmt.Printf("  • %-12s [%s] found %d active log source(s)\n", name, status, sourceCount)
		}
	},
}
