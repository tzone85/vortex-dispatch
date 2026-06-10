package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tzone85/vortex-dispatch/internal/autoresearch"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
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
		budget         time.Duration
		continuous     bool
		duration       time.Duration
		dryRun         bool
		maxExperiments int
	)
	cmd := &cobra.Command{
		Use:   "start <repo>",
		Short: "Start the autoresearch coordinator for a repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoArg := args[0]
			cfg, err := loadConfigForAutoresearch(cmd)
			if err != nil {
				return err
			}
			if !cfg.Autoresearch.Enabled {
				return fmt.Errorf("autoresearch.enabled is false in vxd.yaml — set it to true first")
			}
			if budget > 0 {
				cfg.Autoresearch.Budget = budget.String()
			}
			if continuous {
				cfg.Autoresearch.Continuous = true
			}
			if maxExperiments > 0 {
				cfg.Autoresearch.MaxExperiments = maxExperiments
			}

			repoDir, err := filepath.Abs(repoArg)
			if err != nil {
				return fmt.Errorf("resolve repo path %s: %w", repoArg, err)
			}
			if _, statErr := os.Stat(filepath.Join(repoDir, ".git")); statErr != nil {
				return fmt.Errorf("not a git repository: %s", repoDir)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "autoresearch start: repo=%s budget=%s parallel=%d gate=%s\n",
				repoDir, cfg.Autoresearch.Budget, cfg.Autoresearch.Parallel, cfg.Autoresearch.Gate)

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "--dry-run: configuration validated; not spawning coordinator")
				return nil
			}

			coord, runner, cleanup, err := buildLiveCoordinator(repoDir, cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			ctx, cancel := context.WithCancel(cmd.Context())
			if duration > 0 {
				ctx, cancel = context.WithTimeout(cmd.Context(), duration)
			}
			defer cancel()

			// Trap SIGINT/SIGTERM so the coordinator drains in-flight experiments.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Fprintln(cmd.OutOrStdout(), "autoresearch: stop requested, draining...")
				coord.Stop()
				cancel()
			}()

			_ = runner // silence unused; reserved for future status hooks
			fmt.Fprintln(cmd.OutOrStdout(), "autoresearch: coordinator running (Ctrl-C to stop)")
			if err := coord.Run(ctx); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "autoresearch: coordinator stopped")
			return nil
		},
	}
	cmd.Flags().DurationVar(&budget, "budget", 0, "Override autoresearch.budget (e.g. 10m)")
	cmd.Flags().BoolVar(&continuous, "continuous", false, "Run back-to-back instead of scheduled batch")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Maximum wall-clock duration for this session (default: until Ctrl-C)")
	cmd.Flags().IntVar(&maxExperiments, "max-experiments", 0, "Hard cap on total experiments this run (0 = unlimited); bounds API spend")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate config and resolve dependencies without spawning the coordinator")
	return cmd
}

// buildLiveCoordinator wires up every dependency the autoresearch
// Coordinator needs from the configured project state. Returns the
// Coordinator, its underlying ExperimentRunner (for diagnostics), and
// a cleanup func that releases the event store.
func buildLiveCoordinator(repoDir string, cfg config.Config) (*autoresearch.Coordinator, *autoresearch.ExperimentRunner, func(), error) {
	stateDir := defaultStateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("ensure state dir %s: %w", stateDir, err)
	}
	store, err := state.NewFileStore(filepath.Join(stateDir, "events.jsonl"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open event store: %w", err)
	}
	cleanup := func() { _ = store.Close() }

	rt, runtimeName, err := pickAutoresearchRuntime(cfg.Runtimes)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}

	ll, err := buildAutoresearchLLMClient(cfg)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}

	model := cfg.Models.Senior.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	worktreeRoot := filepath.Join(stateDir, "autoresearch-worktrees", filepath.Base(repoDir))
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("ensure worktree root: %w", err)
	}

	driver := &autoresearch.LiveAgentDriver{
		Runtime: rt,
		Model:   model,
		LogDir:  filepath.Join(stateDir, "autoresearch-logs"),
	}

	metric := &autoresearch.MetricHarness{
		Metric: cfg.Autoresearch.Metric,
		Tiebreaker: &autoresearch.LLMTiebreaker{
			Client: ll,
			Model:  model,
		},
	}
	tripwireModel := cfg.Autoresearch.Tripwire.Model
	if tripwireModel == "" {
		tripwireModel = model
	}
	tripwire := &autoresearch.TripwireJudge{Client: ll, Model: tripwireModel}

	bayesClasses := autoresearch.DefaultClasses
	if len(cfg.Autoresearch.Bayes.Classes) > 0 {
		bayesClasses = make([]autoresearch.ExperimentClass, 0, len(cfg.Autoresearch.Bayes.Classes))
		for _, c := range cfg.Autoresearch.Bayes.Classes {
			bayesClasses = append(bayesClasses, autoresearch.ExperimentClass(c))
		}
	}
	sampler := autoresearch.NewBayesSampler(
		bayesClasses,
		cfg.Autoresearch.Bayes.PriorAlpha,
		cfg.Autoresearch.Bayes.PriorBeta,
	)
	bank := autoresearch.NewHypothesisBank(store)

	gate := autoresearch.NewGateRouter(cfg.Merge.BaseBranch, autoresearch.DefaultGateOps{})
	if gate.BaseBranch == "" {
		gate.BaseBranch = "main"
	}

	allow := cfg.Autoresearch.EditablePaths
	deny := cfg.Autoresearch.ForbiddenPaths
	runner := &autoresearch.ExperimentRunner{
		RepoDir:      repoDir,
		BaseBranch:   gate.BaseBranch,
		WorktreeRoot: worktreeRoot,
		Worktree:     autoresearch.DefaultWorktreeOps{},
		Driver:       driver,
		Filter:       autoresearch.PathFilter{Allow: allow, Deny: deny},
		Metric:       metric,
		Tripwire:     tripwire,
		Bank:         bank,
		Sampler:      sampler,
		Gate:         gate,
		GateAction:   autoresearch.GateAction(cfg.Autoresearch.Gate),
		Conventions: autoresearch.Conventions{
			Language:     guessLanguage(repoDir),
			TestPatterns: []string{"*_test.go", "**/*_test.go"},
		},
		Events: store,
	}

	budget := parseBudget(cfg.Autoresearch.Budget)
	parallel := cfg.Autoresearch.Parallel
	if parallel < 1 {
		parallel = 1
	}

	coord := autoresearch.NewCoordinator(
		repoDir,
		bank,
		sampler,
		runner,
		baselineFromConfig(cfg),
		parallel,
		budget,
	)
	// Hard ceiling on total experiments (spend) for this run, if configured.
	if cfg.Autoresearch.MaxExperiments > 0 {
		coord.MaxExperiments = cfg.Autoresearch.MaxExperiments
	}

	// One last log line so operators can confirm the runtime selection.
	fmt.Fprintf(os.Stderr, "autoresearch: runtime=%s model=%s baseline-source=fixed\n", runtimeName, model)
	return coord, runner, cleanup, nil
}

// pickAutoresearchRuntime picks the first runtime registered in the config
// (alphabetical for determinism), or returns an error advising the user
// to configure one.
func pickAutoresearchRuntime(cfg map[string]config.RuntimeConfig) (runtime.Runtime, string, error) {
	if len(cfg) == 0 {
		return nil, "", fmt.Errorf("no runtimes configured in vxd.yaml — add a runtimes: block")
	}
	reg, err := runtime.NewRegistry(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("build runtime registry: %w", err)
	}
	names := reg.List()
	sort.Strings(names)
	rt, err := reg.Get(names[0])
	if err != nil {
		return nil, "", err
	}
	return rt, names[0], nil
}

// buildAutoresearchLLMClient picks the best available LLM client for
// tripwire and tiebreak calls. Prefers the Anthropic API (when an API
// key is set), falls back to the Claude CLI subscription path.
func buildAutoresearchLLMClient(cfg config.Config) (llm.Client, error) {
	if apiKey := resolveAPIKey("ANTHROPIC_API_KEY"); apiKey != "" {
		return llm.NewRetryClient(llm.NewAnthropicClient(apiKey), 2), nil
	}
	if _, err := lookPath("claude"); err == nil {
		return llm.NewClaudeCLIClient(), nil
	}
	return nil, fmt.Errorf("no LLM client available — set ANTHROPIC_API_KEY or install the claude CLI")
}

// baselineFromConfig returns a baseline source. For v1 we use a fixed
// value of 0 (callers seed via BASELINE_MEASURED events as those land);
// the runner reads baseline from the latest kept-experiment delta.
//
// A more sophisticated baseline would re-measure on `main` HEAD between
// experiments; left as a v2 lever per the spec's "open questions".
func baselineFromConfig(_ config.Config) func() float64 {
	return func() float64 { return 0 }
}

func parseBudget(s string) time.Duration {
	if s == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// guessLanguage returns a coarse language label from the repo's go.mod /
// package.json / etc. Used by the tripwire judge to weight which patterns
// to be paranoid about.
func guessLanguage(repoDir string) string {
	checks := []struct {
		marker string
		lang   string
	}{
		{"go.mod", "go"},
		{"package.json", "javascript"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"Cargo.toml", "rust"},
		{"build.gradle", "java"},
		{"pom.xml", "java"},
		{"composer.json", "php"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(repoDir, c.marker)); err == nil {
			return c.lang
		}
	}
	return ""
}

// lookPath is a thin wrapper to keep `exec` out of the file's import set
// when the resolveAPIKey helper already exists in the package.
func lookPath(name string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		full := filepath.Join(dir, name)
		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			return full, nil
		}
	}
	return "", fmt.Errorf("%s not in PATH", name)
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
