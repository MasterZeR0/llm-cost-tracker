package cli

import (
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
)

func TestDashboardCmd_PortFlag(t *testing.T) {
	flag := dashboardCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected --port flag to be registered on dashboardCmd")
	}
	if flag.DefValue != "8384" {
		t.Fatalf("expected default port to be 8384, got %s", flag.DefValue)
	}

	cfg = config.DefaultConfig()
	cfg.Dashboard.Port = 8384

	// Set custom port flag
	if err := dashboardCmd.Flags().Set("port", "9090"); err != nil {
		t.Fatal(err)
	}

	if dashboardCmd.PreRun != nil {
		dashboardCmd.PreRun(dashboardCmd, []string{})
	}

	if dashboardPort != 9090 {
		t.Fatalf("expected dashboardPort to be 9090, got %d", dashboardPort)
	}
}

func TestDaemonCmd_ForegroundFlagAndDefaultRunE(t *testing.T) {
	flag := daemonCmd.PersistentFlags().Lookup("foreground")
	if flag == nil {
		t.Fatal("expected persistent --foreground flag on daemonCmd")
	}

	shorthand := daemonCmd.PersistentFlags().ShorthandLookup("f")
	if shorthand == nil {
		t.Fatal("expected -f shorthand flag on daemonCmd")
	}

	if daemonCmd.RunE == nil {
		t.Fatal("expected daemonCmd to have RunE configured to run daemonStartCmd")
	}

	// Verify start subcommand inherits persistent flag
	startFlag := daemonStartCmd.Flag("foreground")
	if startFlag == nil {
		t.Fatal("expected daemonStartCmd to inherit --foreground flag")
	}
}
