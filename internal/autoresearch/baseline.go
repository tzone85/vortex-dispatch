package autoresearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Baseline identifies a metric measurement for a specific repository state and
// evaluator configuration. The identity fields make persisted measurements
// safe to reuse only when the ruler and the code being measured are unchanged.
type Baseline struct {
	Repo          string    `json:"repo"`
	BaseRef       string    `json:"base_ref"`
	BaseCommit    string    `json:"base_commit"`
	MetricHash    string    `json:"metric_hash"`
	EvaluatorHash string    `json:"evaluator_hash"`
	Score         Score     `json:"score"`
	MeasuredAt    time.Time `json:"measured_at"`
}

// BaselineProvider supplies the baseline that experiments compare against.
type BaselineProvider interface {
	Current(ctx context.Context) (Baseline, error)
}

// BaselineProviderFunc adapts a function to BaselineProvider. It is useful for
// deterministic tests and small integrations.
type BaselineProviderFunc func(ctx context.Context) (Baseline, error)

// Current invokes f.
func (f BaselineProviderFunc) Current(ctx context.Context) (Baseline, error) {
	return f(ctx)
}

// EventBaselineProvider measures a base ref and caches the result in the
// append-only event store. Cache identity includes the resolved commit, metric
// configuration, evaluator configuration, and repository path.
type EventBaselineProvider struct {
	Repo          string
	BaseRef       string
	Metric        *MetricHarness
	EvaluatorHash string
	Events        state.EventStore
	Now           func() time.Time
	Logf          func(format string, args ...any)

	mu sync.Mutex
}

// MetricConfigHash returns a stable hash of every metric setting that can
// affect measurement behavior.
func MetricConfigHash(metric config.AutoresearchMetric, strictShellCommands bool) string {
	identity := struct {
		Command             string                          `json:"command"`
		Parser              config.AutoresearchMetricParser `json:"parser"`
		TieEpsilon          float64                         `json:"tie_epsilon"`
		TiebreakRubric      string                          `json:"tiebreak_rubric"`
		StrictShellCommands bool                            `json:"strict_shell_commands"`
	}{
		Command:             metric.Command,
		Parser:              metric.Parser,
		TieEpsilon:          metric.TieEpsilon,
		TiebreakRubric:      metric.TiebreakRubric,
		StrictShellCommands: strictShellCommands,
	}
	data, err := json.Marshal(identity)
	if err != nil {
		panic(fmt.Sprintf("marshal metric identity: %v", err))
	}
	return hashBytes(data)
}

// EvaluatorConfigHash returns a stable, non-secret identifier for evaluator
// configuration such as the selected model/version.
func EvaluatorConfigHash(identity string) string {
	return hashBytes([]byte(identity))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Current returns the latest matching persisted baseline or measures and
// persists a new one when any identity component changed.
func (p *EventBaselineProvider) Current(ctx context.Context) (Baseline, error) {
	if p == nil {
		return Baseline{}, errors.New("baseline provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Repo == "" {
		return Baseline{}, errors.New("baseline repository is empty")
	}
	if p.BaseRef == "" {
		return Baseline{}, errors.New("baseline base ref is empty")
	}
	if p.Metric == nil {
		return Baseline{}, errors.New("baseline metric is nil")
	}
	if p.Events == nil {
		return Baseline{}, errors.New("baseline event store is nil")
	}

	commit, err := resolveGitCommit(ctx, p.Repo, p.BaseRef)
	if err != nil {
		return Baseline{}, err
	}
	metricHash := MetricConfigHash(p.Metric.Metric, p.Metric.StrictShellCommands)

	cached, ok, err := p.findCached(commit, metricHash)
	if err != nil {
		return Baseline{}, err
	}
	if ok {
		p.log("baseline source=event-cache repo=%s base_ref=%s base_commit=%s metric_hash=%s evaluator_hash=%s score=%g",
			p.Repo, p.BaseRef, commit, metricHash, p.EvaluatorHash, cached.Score.Final)
		return cached, nil
	}

	baselineMetric := *p.Metric
	baselineMetric.Tiebreaker = nil
	score, err := measureCommit(ctx, p.Repo, commit, &baselineMetric)
	if err != nil {
		return Baseline{}, fmt.Errorf("measure baseline at %s: %w", commit, err)
	}
	// Raw metric output may contain secrets and is intentionally excluded from
	// persisted events. Clear it before returning as well so measured and cached
	// baselines have identical, safe semantics.
	score.RawOutput = ""
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	baseline := Baseline{
		Repo:          p.Repo,
		BaseRef:       p.BaseRef,
		BaseCommit:    commit,
		MetricHash:    metricHash,
		EvaluatorHash: p.EvaluatorHash,
		Score:         score,
		MeasuredAt:    now,
	}
	payload, err := baselinePayload(baseline)
	if err != nil {
		return Baseline{}, err
	}
	if err := p.Events.Append(state.NewEvent(state.EventBaselineMeasured, "autoresearch", "", payload)); err != nil {
		return Baseline{}, fmt.Errorf("persist baseline measurement: %w", err)
	}
	p.log("baseline source=measured repo=%s base_ref=%s base_commit=%s metric_hash=%s evaluator_hash=%s score=%g",
		p.Repo, p.BaseRef, commit, metricHash, p.EvaluatorHash, score.Final)
	return baseline, nil
}

func (p *EventBaselineProvider) findCached(commit, metricHash string) (Baseline, bool, error) {
	events, err := p.Events.List(state.EventFilter{Type: state.EventBaselineMeasured})
	if err != nil {
		return Baseline{}, false, fmt.Errorf("list baseline measurements: %w", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		var candidate Baseline
		if err := json.Unmarshal(events[i].Payload, &candidate); err != nil {
			continue
		}
		if candidate.Repo == p.Repo &&
			candidate.BaseRef == p.BaseRef &&
			candidate.BaseCommit == commit &&
			candidate.MetricHash == metricHash &&
			candidate.EvaluatorHash == p.EvaluatorHash {
			return candidate, true, nil
		}
	}
	return Baseline{}, false, nil
}

func baselinePayload(baseline Baseline) (map[string]any, error) {
	data, err := json.Marshal(baseline)
	if err != nil {
		return nil, fmt.Errorf("marshal baseline: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("encode baseline payload: %w", err)
	}
	return payload, nil
}

func resolveGitCommit(ctx context.Context, repo, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve base ref %q: %w (%s)", ref, err, strings.TrimSpace(string(out)))
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", fmt.Errorf("resolve base ref %q: empty commit", ref)
	}
	return commit, nil
}

func measureCommit(ctx context.Context, repo, commit string, metric *MetricHarness) (Score, error) {
	root, err := os.MkdirTemp("", "vxd-baseline-")
	if err != nil {
		return Score{}, fmt.Errorf("create baseline worktree root: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	worktree := filepath.Join(root, "checkout")
	add := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktree, commit)
	add.Dir = repo
	if out, err := add.CombinedOutput(); err != nil {
		return Score{}, fmt.Errorf("materialize baseline commit: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	score, measureErr := metric.Measure(ctx, worktree, 0, "")
	cleanupErr := removeWorktree(repo, worktree)
	if measureErr != nil || cleanupErr != nil {
		return Score{}, errors.Join(measureErr, cleanupErr)
	}
	return score, nil
}

func removeWorktree(repo, worktree string) error {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCleanup()
	remove := exec.CommandContext(cleanupCtx, "git", "worktree", "remove", "--force", worktree)
	remove.Dir = repo
	removeOut, removeErr := remove.CombinedOutput()
	if removeErr == nil {
		return nil
	}

	// If normal removal fails, delete the checkout and prune its Git metadata.
	// Return the original failure even when fallback cleanup succeeds so callers
	// never mistake an abnormal cleanup for a clean measurement.
	removePathErr := os.RemoveAll(worktree)
	pruneCtx, cancelPrune := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelPrune()
	prune := exec.CommandContext(pruneCtx, "git", "worktree", "prune", "--expire", "now")
	prune.Dir = repo
	pruneOut, pruneErr := prune.CombinedOutput()

	cleanupErr := fmt.Errorf("remove baseline worktree: %w (%s)", removeErr, strings.TrimSpace(string(removeOut)))
	if removePathErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete baseline checkout: %w", removePathErr))
	}
	if pruneErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("prune baseline worktree metadata: %w (%s)", pruneErr, strings.TrimSpace(string(pruneOut))))
	}
	return cleanupErr
}

func (p *EventBaselineProvider) log(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}
