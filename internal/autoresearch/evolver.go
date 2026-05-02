package autoresearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// WorkspaceWriter persists file content onto a branch via a one-shot
// commit. Production impl creates an ephemeral worktree, writes the
// files, commits, and removes the worktree. Tests inject fakes.
type WorkspaceWriter interface {
	WriteAndCommit(repoDir, branch, message string, files map[string]string) error
}

// ProgramMDEvolver rewrites a repo's program.md based on accumulated wins/losses.
//
// HARDCODED HUMAN REVIEW: never auto-merges, regardless of the repo's gate
// setting. Always opens a PR.
type ProgramMDEvolver struct {
	Client          llm.Client
	Model           string
	Bank            *HypothesisBank
	GateOps         GateOps         // for opening the PR
	Workspace       WorkspaceWriter // writes the new program.md onto the branch
	ProgramMDPath   string          // relative path within repo, defaults to "program.md"
	BaseBranch      string
	Events          EventSink
	Now             func() time.Time
}

const evolverSystem = `You are evolving the program.md instructions for an autoresearch coding agent.

You will be given:
- The current program.md (free-form Markdown that instructs the agent).
- A list of past wins (kept experiments) and losses (discarded/tripwired).

Your job: rewrite program.md so future experiments are more likely to succeed.
- Keep the file under 200 lines.
- Bias toward instructions, not narration.
- Reference patterns that produced wins; warn against patterns that produced losses.
- Preserve sections the human added unless wins/losses contradict them.

Output ONLY the new program.md content. No code fences, no explanation.`

// Evolve runs one evolution cycle. Returns the PR URL on success or
// (empty, nil) when there were no significant changes.
func (e *ProgramMDEvolver) Evolve(ctx context.Context, repoDir, repo, currentMD string) (string, error) {
	if e.Client == nil {
		return "", errors.New("evolver: LLM client is nil")
	}
	wins, _ := e.Bank.TopWins(repo, 20)
	losses, _ := e.Bank.TopLosses(repo, 20)

	user := buildEvolverPrompt(currentMD, wins, losses)
	resp, err := e.Client.Complete(ctx, llm.CompletionRequest{
		Model:       e.Model,
		MaxTokens:   4096,
		Temperature: 0.2,
		System:      evolverSystem,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("evolver LLM: %w", err)
	}
	newMD := strings.TrimSpace(resp.Content)
	if newMD == "" || newMD == strings.TrimSpace(currentMD) {
		return "", nil
	}

	// Open PR via gate ops. The branch convention is autoresearch/evolve-{date}.
	branch := "autoresearch/evolve-" + e.now().Format("20060102")
	if err := e.GateOps.CreateBranch(repoDir, branch); err != nil {
		return "", fmt.Errorf("create branch %s: %w", branch, err)
	}

	// Persist the new program.md onto the branch via the WorkspaceWriter
	// before pushing. Without this, the push would have no commits and the
	// resulting PR would be empty.
	if e.Workspace != nil {
		programPath := e.ProgramMDPath
		if programPath == "" {
			programPath = "program.md"
		}
		commitMsg := "autoresearch: program.md evolution " + e.now().Format("2006-01-02")
		if err := e.Workspace.WriteAndCommit(repoDir, branch, commitMsg, map[string]string{
			programPath: newMD + "\n",
		}); err != nil {
			return "", fmt.Errorf("write program.md on %s: %w", branch, err)
		}
	}

	if err := e.GateOps.PushBranch(repoDir, branch); err != nil {
		return "", fmt.Errorf("push %s: %w", branch, err)
	}
	body := buildEvolverPRBody(wins, losses)
	url, err := e.GateOps.CreatePR(repoDir, "autoresearch: program.md evolution "+e.now().Format("2006-01-02"), body, e.BaseBranch, branch)
	if err != nil {
		return "", err
	}
	e.emit(state.EventProgrammdEvolved, map[string]any{
		"repo":             repo,
		"pr_url":           url,
		"kept_for_review":  true,
		"wins_considered":  len(wins),
		"losses_considered": len(losses),
	})
	return url, nil
}

func (e *ProgramMDEvolver) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

func (e *ProgramMDEvolver) emit(t state.EventType, payload map[string]any) {
	if e.Events == nil {
		return
	}
	evt := state.NewEvent(t, "autoresearch", "", payload)
	_ = e.Events.Append(evt)
}

func buildEvolverPrompt(currentMD string, wins, losses []Experiment) string {
	var b strings.Builder
	b.WriteString("CURRENT program.md:\n")
	b.WriteString(currentMD)
	b.WriteString("\n\nWINS (last 20):\n")
	for _, w := range wins {
		fmt.Fprintf(&b, "- class=%s Δ=%+.4g hash=%s\n", w.Class, w.Delta, shortHash(w.DiffHash))
	}
	b.WriteString("\nLOSSES (last 20):\n")
	for _, l := range losses {
		fmt.Fprintf(&b, "- class=%s reason=%s hash=%s\n", l.Class, l.FailReason, shortHash(l.DiffHash))
	}
	return b.String()
}

func buildEvolverPRBody(wins, losses []Experiment) string {
	var b strings.Builder
	b.WriteString("## program.md evolution\n\n")
	b.WriteString("This PR was opened by the autoresearch evolver after analyzing recent experiments.\n\n")
	b.WriteString("**This PR is NEVER auto-merged.** Human review is hardcoded.\n\n")
	fmt.Fprintf(&b, "Wins considered: %d\nLosses considered: %d\n\n", len(wins), len(losses))
	if len(wins) > 0 {
		b.WriteString("### Top wins\n")
		for _, w := range wins {
			fmt.Fprintf(&b, "- `%s` Δ=%+.4g (`%s`)\n", w.Class, w.Delta, shortHash(w.DiffHash))
		}
	}
	if len(losses) > 0 {
		b.WriteString("\n### Recent losses\n")
		for _, l := range losses {
			fmt.Fprintf(&b, "- `%s` reason=%s (`%s`)\n", l.Class, l.FailReason, shortHash(l.DiffHash))
		}
	}
	return b.String()
}
