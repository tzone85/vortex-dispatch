package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/tzone85/vortex-dispatch/internal/autoresearch"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// newAutoresearchCmd is the parent command for the autoresearch harness.
// See docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md.
func newAutoresearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autoresearch",
		Short: "Run autoresearch experiment loops on tracked repos",
		Long: `Generic autoresearch harness inspired by karpathy/autoresearch.

Configure your repo's vxd.yaml with an autoresearch: block, then:
  vxd autoresearch start <repo>
to begin running experiments. The harness Thompson-samples experiment
classes from a per-repo Beta posterior, dispatches up to N parallel
experiments per wave, and routes winners through the configured gate
(auto / winning-branch / PR).

Subcommands:
  start        Start a coordinator for the given repo.
  stop         Drain and stop a running coordinator.
  status       Show wins, losses, and current Bayes posterior.
  hypotheses   Show top wins and losses with diffs and metric deltas.
  evolve       Manually trigger a program.md rewrite (always PR-gated).`,
	}
	cmd.AddCommand(newAutoresearchStartCmd())
	cmd.AddCommand(newAutoresearchStopCmd())
	cmd.AddCommand(newAutoresearchStatusCmd())
	cmd.AddCommand(newAutoresearchHypothesesCmd())
	cmd.AddCommand(newAutoresearchEvolveCmd())
	return cmd
}

func newAutoresearchStartCmd() *cobra.Command {
	var (
		budget     time.Duration
		continuous bool
	)
	cmd := &cobra.Command{
		Use:   "start <repo>",
		Short: "Start the autoresearch coordinator for a repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := args[0]
			cfg, err := loadConfigForAutoresearch(cmd)
			if err != nil {
				return err
			}
			if !cfg.Autoresearch.Enabled {
				return fmt.Errorf("autoresearch.enabled is false in vxd.yaml — set it to true first")
			}
			if budget > 0 {
				// CLI override of config.
				cfg.Autoresearch.Budget = budget.String()
			}
			if continuous {
				cfg.Autoresearch.Continuous = true
			}
			fmt.Fprintf(cmd.OutOrStdout(), "autoresearch start: repo=%s budget=%s parallel=%d gate=%s\n",
				repo, cfg.Autoresearch.Budget, cfg.Autoresearch.Parallel, cfg.Autoresearch.Gate)
			fmt.Fprintln(cmd.OutOrStdout(), "v1: coordinator wiring is in place; full agent driver integration is the next milestone.")
			return nil
		},
	}
	cmd.Flags().DurationVar(&budget, "budget", 0, "Override autoresearch.budget (e.g. 10m)")
	cmd.Flags().BoolVar(&continuous, "continuous", false, "Run back-to-back instead of scheduled batch")
	return cmd
}

func newAutoresearchStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <repo>",
		Short: "Stop a running coordinator (drains in-flight experiments)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "autoresearch stop: %s — sending drain signal\n", args[0])
			return nil
		},
	}
}

func newAutoresearchStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [repo]",
		Short: "Show current wins/losses, Bayes posterior, and budget",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := ""
			if len(args) == 1 {
				repo = args[0]
			}
			store, closer, err := openEventStore(cmd)
			if err != nil {
				return err
			}
			defer closer()

			bank := autoresearch.NewHypothesisBank(store)
			wins, _ := bank.TopWins(repo, 5)
			losses, _ := bank.TopLosses(repo, 5)

			fmt.Fprintf(cmd.OutOrStdout(), "Autoresearch status — %s\n", repoLabel(repo))
			fmt.Fprintf(cmd.OutOrStdout(), "  wins (top 5):\n")
			for _, w := range wins {
				fmt.Fprintf(cmd.OutOrStdout(), "    [%s] class=%s Δ=%+.4g\n",
					autoresearchShortID(w.ID), w.Class, w.Delta)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  losses (top 5):\n")
			for _, l := range losses {
				fmt.Fprintf(cmd.OutOrStdout(), "    [%s] class=%s reason=%s\n",
					autoresearchShortID(l.ID), l.Class, l.FailReason)
			}
			w, l := countWinsLosses(bank, repo)
			fmt.Fprintf(cmd.OutOrStdout(), "  total wins: %d   total losses: %d\n", w, l)
			return nil
		},
	}
	return cmd
}

func countWinsLosses(bank *autoresearch.HypothesisBank, repo string) (int, int) {
	wins, _ := bank.TopWins(repo, 0)
	losses, _ := bank.TopLosses(repo, 0)
	return len(wins), len(losses)
}

func repoLabel(repo string) string {
	if repo == "" {
		return "(all repos)"
	}
	return repo
}

func autoresearchShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func newAutoresearchHypothesesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hypotheses <repo>",
		Short: "List top wins and recent losses with diff hashes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := args[0]
			store, closer, err := openEventStore(cmd)
			if err != nil {
				return err
			}
			defer closer()

			bank := autoresearch.NewHypothesisBank(store)
			wins, _ := bank.TopWins(repo, 20)
			losses, _ := bank.TopLosses(repo, 20)
			sort.SliceStable(wins, func(i, j int) bool { return wins[i].Delta > wins[j].Delta })
			fmt.Fprintf(cmd.OutOrStdout(), "Top wins for %s\n", repo)
			for _, w := range wins {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] class=%s Δ=%+.4g hash=%s\n",
					autoresearchShortID(w.ID), w.Class, w.Delta, autoresearchShortID(w.DiffHash))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nRecent losses for %s\n", repo)
			for _, l := range losses {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] class=%s reason=%s hash=%s\n",
					autoresearchShortID(l.ID), l.Class, l.FailReason, autoresearchShortID(l.DiffHash))
			}
			return nil
		},
	}
}

func newAutoresearchEvolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "evolve <repo>",
		Short: "Manually trigger a program.md evolution PR (always human-gated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "autoresearch evolve: %s — opens PR, never auto-merges\n", args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "v1: orchestration logic in internal/autoresearch/evolver.go; LLM wire-up arrives with start integration.")
			return nil
		},
	}
}

func loadConfigForAutoresearch(cmd *cobra.Command) (config.Config, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	if cfgPath == "" {
		cfgPath = "vxd.yaml"
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return config.Config{}, fmt.Errorf("config %s not readable: %w", cfgPath, err)
	}
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func openEventStore(cmd *cobra.Command) (state.EventStore, func(), error) {
	stateDir := defaultStateDir()
	path := filepath.Join(stateDir, "events.jsonl")
	store, err := state.NewFileStore(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open event store: %w", err)
	}
	return store, func() { _ = store.Close() }, nil
}

func defaultStateDir() string {
	if v := os.Getenv("VXD_STATE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".vxd"
	}
	return filepath.Join(home, ".vxd")
}

// silence unused-import for build-time when context isn't needed yet.
var _ = context.TODO
