package autoresearch

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/tzone85/vortex-dispatch/internal/git"
)

// GateAction is the kind of acceptance routing applied to a kept experiment.
type GateAction string

const (
	GateAuto    GateAction = "auto"    // squash-merge to base
	GateWinning GateAction = "winning" // fast-forward onto autoresearch/winning
	GatePR      GateAction = "pr"      // open PR for human review
)

// GateOps abstracts the git operations the GateRouter needs. Real impl uses
// the internal/git package; tests inject a fake.
type GateOps interface {
	CreateBranch(repoDir, name string) error
	BranchExists(repoDir, name string) bool
	PushBranch(repoDir, branch string) error
	CreatePR(repoDir, title, body, baseBranch, headBranch string) (string, error)
	MergePR(repoDir, prURL string) error
	RebaseOnto(worktreePath, upstream string) error
	FastForwardWinning(repoDir, sourceBranch, winningBranch string) error
}

// DefaultGateOps wraps the real internal/git functions.
type DefaultGateOps struct{}

// CreateBranch creates a branch in the given repo.
func (DefaultGateOps) CreateBranch(repoDir, name string) error { return git.CreateBranch(repoDir, name) }

// BranchExists reports whether a branch exists.
func (DefaultGateOps) BranchExists(repoDir, name string) bool { return git.BranchExists(repoDir, name) }

// PushBranch pushes the given branch to origin.
func (DefaultGateOps) PushBranch(repoDir, branch string) error { return git.PushBranch(repoDir, branch) }

// CreatePR opens a PR and returns its URL.
func (DefaultGateOps) CreatePR(repoDir, title, body, baseBranch, headBranch string) (string, error) {
	pr, err := git.CreatePR(repoDir, title, body, baseBranch, headBranch)
	if err != nil {
		return "", err
	}
	return pr.URL, nil
}

// MergePR squashes a PR by URL. Parses the PR number from the GitHub URL
// (as returned by CreatePR) and delegates to the real git.MergePR (which
// takes int). This makes the 'auto' gate produce correct outcomes on the
// shipped DefaultGateOps path.
func (DefaultGateOps) MergePR(repoDir, prURL string) error {
	num, err := parsePRNumberFromURL(prURL)
	if err != nil {
		return err
	}
	return git.MergePR(repoDir, num)
}

// parsePRNumberFromURL extracts the trailing PR number from a GitHub PR URL.
// Handles common forms: .../pull/123 , .../pull/123/ , with or without .git etc.
func parsePRNumberFromURL(u string) (int, error) {
	if u == "" {
		return 0, errors.New("MergePR: empty PR URL")
	}
	// strip trailing slash and query/fragment
	u = strings.Split(u, "?")[0]
	u = strings.Split(u, "#")[0]
	u = strings.TrimSuffix(u, "/")
	parts := strings.Split(u, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(parts[i]); err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("MergePR: could not parse PR number from URL %q", u)
}

// RebaseOnto rebases the current worktree branch onto upstream.
func (DefaultGateOps) RebaseOnto(worktreePath, upstream string) error {
	return git.RebaseOnto(worktreePath, upstream)
}

// FastForwardWinning advances the autoresearch/winning branch to source.
// Implemented as a non-fast-forward-safe checkout+reset on the bare repo.
// In v1 we delegate to a helper that uses `git fetch . source:winning`,
// which is a fast-forward update of one local ref to another.
func (DefaultGateOps) FastForwardWinning(repoDir, sourceBranch, winningBranch string) error {
	return git.FastForwardLocal(repoDir, sourceBranch, winningBranch)
}

// GateRouter dispatches a kept Outcome onto the configured gate.
//
// Concurrency: the winning-branch fast-forward must be serialized per repo
// to avoid two simultaneous experiments racing. We hold a mutex per repoDir.
type GateRouter struct {
	BaseBranch    string
	WinningBranch string // default "autoresearch/winning"
	Ops           GateOps

	mu     sync.Mutex
	locks  map[string]*sync.Mutex
}

// NewGateRouter constructs a router with the standard winning branch name.
func NewGateRouter(baseBranch string, ops GateOps) *GateRouter {
	if ops == nil {
		ops = DefaultGateOps{}
	}
	return &GateRouter{
		BaseBranch:    baseBranch,
		WinningBranch: "autoresearch/winning",
		Ops:           ops,
		locks:         map[string]*sync.Mutex{},
	}
}

func (r *GateRouter) lockFor(repoDir string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.locks[repoDir]; ok {
		return l
	}
	l := &sync.Mutex{}
	r.locks[repoDir] = l
	return l
}

// Route applies the configured gate to the outcome. On success, returns
// the action and the resulting branch or PR URL; otherwise an error.
func (r *GateRouter) Route(repoDir string, action GateAction, exp Outcome) (string, error) {
	if !exp.Kept {
		return "", errors.New("GateRouter: cannot route a non-kept outcome")
	}
	switch action {
	case GateAuto:
		return r.routeAuto(repoDir, exp)
	case GateWinning:
		return r.routeWinning(repoDir, exp)
	case GatePR:
		return r.routePR(repoDir, exp)
	default:
		return "", fmt.Errorf("GateRouter: unknown gate action %q", action)
	}
}

// routeWinning fast-forwards autoresearch/winning to the experiment branch.
// Holds a per-repo lock so concurrent winners are serialized.
func (r *GateRouter) routeWinning(repoDir string, exp Outcome) (string, error) {
	lock := r.lockFor(repoDir)
	lock.Lock()
	defer lock.Unlock()

	if !r.Ops.BranchExists(repoDir, r.WinningBranch) {
		if err := r.Ops.CreateBranch(repoDir, r.WinningBranch); err != nil {
			return "", fmt.Errorf("create %s: %w", r.WinningBranch, err)
		}
	}
	src := experimentBranch(exp)
	if err := r.Ops.FastForwardWinning(repoDir, src, r.WinningBranch); err != nil {
		return "", fmt.Errorf("fast-forward %s → %s: %w", src, r.WinningBranch, err)
	}
	return r.WinningBranch, nil
}

// routePR opens a PR and returns its URL.
func (r *GateRouter) routePR(repoDir string, exp Outcome) (string, error) {
	src := experimentBranch(exp)
	if err := r.Ops.PushBranch(repoDir, src); err != nil {
		return "", fmt.Errorf("push %s: %w", src, err)
	}
	title := fmt.Sprintf("autoresearch: %s — Δ %.4g", exp.Proposal.Class, exp.Delta)
	body := buildPRBody(exp)
	url, err := r.Ops.CreatePR(repoDir, title, body, r.BaseBranch, src)
	if err != nil {
		return "", err
	}
	return url, nil
}

// routeAuto opens a PR and squash-merges it. We never bypass GitHub's PR
// flow even on auto-gate, so the merge is auditable.
func (r *GateRouter) routeAuto(repoDir string, exp Outcome) (string, error) {
	url, err := r.routePR(repoDir, exp)
	if err != nil {
		return "", err
	}
	if err := r.Ops.MergePR(repoDir, url); err != nil {
		return url, fmt.Errorf("merge PR %s: %w", url, err)
	}
	return url, nil
}

func experimentBranch(exp Outcome) string {
	if exp.BranchOrPR != "" {
		return exp.BranchOrPR
	}
	return "autoresearch/exp-" + exp.Proposal.ID
}

func buildPRBody(exp Outcome) string {
	return fmt.Sprintf(`Autoresearch experiment **%s** (class: %s)

| | |
|---|---|
| Baseline | %.6g |
| Score | %.6g |
| Δ | %+.4g |
| Verdict | %s |
| Tripwire | %s |

%s

> Generated by VXD autoresearch harness. See docs/superpowers/specs/2026-05-02-autoresearch-harness-design.md.
`, exp.Proposal.ID, exp.Proposal.Class, exp.Baseline, exp.Score.Final, exp.Delta, exp.Verdict, exp.VerdictNote, exp.Proposal.PromptHash)
}
