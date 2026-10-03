package autoresearch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestEventBaselineProvider_MeasuresThenReusesMatchingEvent(t *testing.T) {
	repo := initBaselineTestRepo(t)
	store, err := state.NewFileStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	countFile := filepath.Join(t.TempDir(), "measurements")
	metric := &MetricHarness{Metric: config.AutoresearchMetric{
		Command: fmt.Sprintf("printf x >> %q; printf 'score=42.5\\n'", countFile),
		Parser: config.AutoresearchMetricParser{
			Kind:          "regex",
			Pattern:       `score=([0-9.]+)`,
			LowerIsBetter: true,
		},
		TieEpsilon:     0.01,
		TiebreakRubric: "prefer simpler changes",
	}}
	measuredAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	var logs []string
	provider := &EventBaselineProvider{
		Repo:          repo,
		BaseRef:       "main",
		Metric:        metric,
		EvaluatorHash: EvaluatorConfigHash("judge-v1"),
		Events:        store,
		Now:           func() time.Time { return measuredAt },
		Logf:          func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	}

	first, err := provider.Current(context.Background())
	if err != nil {
		t.Fatalf("measure baseline: %v", err)
	}
	if first.Score.Final != 42.5 || !first.Score.LowerIsBetter {
		t.Fatalf("unexpected measured score: %+v", first.Score)
	}
	if first.BaseCommit == "" || first.MetricHash == "" || first.EvaluatorHash == "" {
		t.Fatalf("baseline identity is incomplete: %+v", first)
	}
	if !first.MeasuredAt.Equal(measuredAt) {
		t.Fatalf("MeasuredAt = %v, want %v", first.MeasuredAt, measuredAt)
	}

	second, err := provider.Current(context.Background())
	if err != nil {
		t.Fatalf("reuse baseline: %v", err)
	}
	if second != first {
		t.Fatalf("cached baseline differs:\n first=%+v\nsecond=%+v", first, second)
	}
	if got := readBaselineMeasurementCount(t, countFile); got != 1 {
		t.Fatalf("metric ran %d times, want 1", got)
	}
	events, err := store.List(state.EventFilter{Type: state.EventBaselineMeasured})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("BASELINE_MEASURED events = %d, want 1", len(events))
	}
	joinedLogs := strings.Join(logs, "\n")
	if !strings.Contains(joinedLogs, "source=measured") || !strings.Contains(joinedLogs, "source=event-cache") {
		t.Fatalf("logs do not distinguish measured/cache sources: %q", joinedLogs)
	}
	if strings.Contains(joinedLogs, metric.Metric.Command) {
		t.Fatalf("log leaked metric command: %q", joinedLogs)
	}
}

func TestEventBaselineProvider_RemeasuresWhenIdentityChanges(t *testing.T) {
	repo := initBaselineTestRepo(t)
	store, err := state.NewFileStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	countFile := filepath.Join(t.TempDir(), "measurements")
	newProvider := func(command, evaluator string) *EventBaselineProvider {
		return &EventBaselineProvider{
			Repo:    repo,
			BaseRef: "main",
			Metric: &MetricHarness{Metric: config.AutoresearchMetric{
				Command: command,
				Parser:  config.AutoresearchMetricParser{Kind: "last_float"},
			}},
			EvaluatorHash: EvaluatorConfigHash(evaluator),
			Events:        store,
		}
	}
	command := fmt.Sprintf("printf x >> %q; printf 10", countFile)
	if _, err := newProvider(command, "judge-v1").Current(context.Background()); err != nil {
		t.Fatal(err)
	}

	commitBaselineTestChange(t, repo, "second")
	if _, err := newProvider(command, "judge-v1").Current(context.Background()); err != nil {
		t.Fatal(err)
	}
	changedMetric := fmt.Sprintf("printf x >> %q; printf 11", countFile)
	if _, err := newProvider(changedMetric, "judge-v1").Current(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := newProvider(changedMetric, "judge-v2").Current(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := readBaselineMeasurementCount(t, countFile); got != 4 {
		t.Fatalf("metric ran %d times, want 4 after commit, metric, and evaluator changes", got)
	}
}

func TestEventBaselineProvider_MeasuresResolvedCommitNotWorkingTree(t *testing.T) {
	repo := initBaselineTestRepo(t)
	commitBaselineTestChange(t, repo, "10")
	baseCommit := strings.TrimSpace(runBaselineGitOutput(t, repo, "rev-parse", "HEAD"))
	commitBaselineTestChange(t, repo, "20")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("30"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := state.NewFileStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	provider := &EventBaselineProvider{
		Repo:    repo,
		BaseRef: baseCommit,
		Metric: &MetricHarness{Metric: config.AutoresearchMetric{
			Command: "cat tracked.txt",
			Parser:  config.AutoresearchMetricParser{Kind: "last_float"},
		}},
		Events: store,
	}
	baseline, err := provider.Current(context.Background())
	if err != nil {
		t.Fatalf("measure baseline: %v", err)
	}
	if baseline.Score.Final != 10 {
		t.Fatalf("baseline score = %g, want 10 from resolved commit; current working tree contains 30", baseline.Score.Final)
	}
}

func TestMetricConfigHash_CoversBehaviorAndIsStable(t *testing.T) {
	base := config.AutoresearchMetric{
		Command:        "go test ./...",
		Parser:         config.AutoresearchMetricParser{Kind: "regex", Pattern: `score: (\\d+)`, LowerIsBetter: true},
		TieEpsilon:     0.01,
		TiebreakRubric: "prefer maintainability",
	}
	first := MetricConfigHash(base, true)
	if first == "" || first != MetricConfigHash(base, true) {
		t.Fatalf("hash is empty or unstable: %q", first)
	}

	variants := []config.AutoresearchMetric{
		func() config.AutoresearchMetric { v := base; v.Command = "go test ./internal/..."; return v }(),
		func() config.AutoresearchMetric { v := base; v.Parser.Kind = "last_float"; return v }(),
		func() config.AutoresearchMetric { v := base; v.Parser.Pattern = `value: (\\d+)`; return v }(),
		func() config.AutoresearchMetric { v := base; v.Parser.LowerIsBetter = false; return v }(),
		func() config.AutoresearchMetric { v := base; v.TieEpsilon = 0.02; return v }(),
		func() config.AutoresearchMetric { v := base; v.TiebreakRubric = "prefer speed"; return v }(),
	}
	for i, variant := range variants {
		if got := MetricConfigHash(variant, true); got == first {
			t.Errorf("variant %d did not change metric hash", i)
		}
	}
	if MetricConfigHash(base, false) == first {
		t.Error("strict shell mode did not change metric hash")
	}
	if EvaluatorConfigHash("judge-v1") == EvaluatorConfigHash("judge-v2") {
		t.Error("evaluator model did not change evaluator hash")
	}
}

func initBaselineTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runBaselineGit(t, repo, "init", "-b", "main")
	runBaselineGit(t, repo, "config", "user.email", "test@example.com")
	runBaselineGit(t, repo, "config", "user.name", "Test")
	commitBaselineTestChange(t, repo, "first")
	return repo
}

func commitBaselineTestChange(t *testing.T, repo, contents string) {
	t.Helper()
	path := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runBaselineGit(t, repo, "add", "tracked.txt")
	runBaselineGit(t, repo, "commit", "-m", contents)
}

func runBaselineGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runBaselineGitOutput(t, dir, args...)
}

func runBaselineGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func readBaselineMeasurementCount(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}
