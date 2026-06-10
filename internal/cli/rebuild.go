package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newRebuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild the SQLite projection from the event log",
		Long: "Reconstructs the materialized SQLite projection by replaying " +
			"events.jsonl, the append-only source of truth. Use this to recover " +
			"when the projection has diverged from the log (e.g. after a crash " +
			"between a durable event append and its projection).",
		RunE: runRebuild,
	}
	cmd.SilenceUsage = true
	return cmd
}

func runRebuild(cmd *cobra.Command, _ []string) error {
	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.Proj.Rebuild(s.Events); err != nil {
		return fmt.Errorf("rebuild projection: %w", err)
	}

	count, _ := s.Events.Count(state.EventFilter{})
	fmt.Fprintf(cmd.OutOrStdout(), "Projection rebuilt from %d events.\n", count)
	return nil
}
