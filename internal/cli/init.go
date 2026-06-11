package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the VXD workspace",
		Long:  "Creates the ~/.vxd/ directory structure, generates a default vxd.yaml config, and initializes stores.",
		RunE:  runInit,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}

	vxdDir := filepath.Join(home, ".vxd")

	// Create directory structure
	dirs := []string{
		vxdDir,
		filepath.Join(vxdDir, "logs"),
		filepath.Join(vxdDir, "worktrees"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	out := cmd.OutOrStdout()

	// Generate vxd.yaml from defaults if not present
	localCfg := "vxd.yaml"
	if _, err := os.Stat(localCfg); os.IsNotExist(err) {
		data, genErr := config.DefaultYAML()
		if genErr != nil {
			return fmt.Errorf("generate default config: %w", genErr)
		}
		if writeErr := os.WriteFile(localCfg, data, 0644); writeErr != nil {
			return fmt.Errorf("write %s: %w", localCfg, writeErr)
		}
		fmt.Fprintf(out, "Created %s with default configuration\n", localCfg)
	} else {
		fmt.Fprintf(out, "Config %s already exists, skipping\n", localCfg)
	}

	// Initialize event store
	eventsPath := filepath.Join(vxdDir, "events.jsonl")
	es, err := state.NewFileStore(eventsPath)
	if err != nil {
		return fmt.Errorf("initialize event store: %w", err)
	}
	if err := es.Close(); err != nil {
		return fmt.Errorf("close event store: %w", err)
	}

	// Initialize projection store (SQLite)
	dbPath := filepath.Join(vxdDir, "vxd.db")
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("initialize projection store: %w", err)
	}
	if err := ps.Close(); err != nil {
		return fmt.Errorf("close projection store: %w", err)
	}

	fmt.Fprintf(out, "Initialized VXD workspace at %s\n", vxdDir)
	fmt.Fprintf(out, "  Event store:      %s\n", eventsPath)
	fmt.Fprintf(out, "  Projection store: %s\n", dbPath)
	fmt.Fprintf(out, "\nRun 'vxd req \"<requirement>\"' to submit your first requirement.\n")

	return nil
}
