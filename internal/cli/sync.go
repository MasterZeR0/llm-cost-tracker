package cli

import (
	"fmt"

	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan all tool directories and import new records once",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scanning tool directories for new usage data...")
		imported, err := daemon.SyncAll(cfg, st, engine)
		if err != nil {
			return err
		}
		fmt.Printf("Sync complete. Imported %d new record(s).\n", imported)
		return nil
	},
}
