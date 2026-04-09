package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
)

func newMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Show pipeline performance metrics",
		Long: `Display success rates, escalation frequency, average times, and per-requirement
breakdowns. Shows the last N requirements (default 10).

Example output:
  Requirements:  12 total, 8 completed
  Stories:       47 total, 38 merged, 5 escalated
  First-pass:    82% (stories that passed review+QA without retries)
  Avg time:      12m 30s per story`,
		RunE: runMetrics,
	}
	cmd.Flags().Int("limit", 10, "Number of recent requirements to analyze")
	cmd.SilenceUsage = true
	return cmd
}

func runMetrics(cmd *cobra.Command, _ []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	limit, _ := cmd.Flags().GetInt("limit")

	s, err := loadStores(cfgPath)
	if err != nil {
		return err
	}
	defer s.Close()

	metrics, err := engine.ComputeMetrics(s.Events, s.Proj, limit)
	if err != nil {
		return fmt.Errorf("compute metrics: %w", err)
	}

	fmt.Fprint(cmd.OutOrStdout(), engine.FormatMetrics(metrics))
	return nil
}
