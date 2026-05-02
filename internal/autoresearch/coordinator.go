package autoresearch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// PromptBuilder produces the agent prompt for a proposal. The default impl
// concatenates a fixed instruction header, the repo's program.md, and a
// rendered "wins/losses" memory seed. Tests inject deterministic builders.
type PromptBuilder interface {
	Build(repo string, class ExperimentClass, programMD string, wins, losses []Experiment) string
}

// SimplePromptBuilder is a deterministic PromptBuilder used as default.
type SimplePromptBuilder struct{}

// Build assembles a prompt with explicit memory-seed sections.
func (SimplePromptBuilder) Build(repo string, class ExperimentClass, programMD string, wins, losses []Experiment) string {
	var s string
	s += "AUTORESEARCH EXPERIMENT\n=====================\n\n"
	s += "Repo: " + repo + "\n"
	s += "Class: " + string(class) + "\n\n"
	if programMD != "" {
		s += "## program.md\n\n" + programMD + "\n\n"
	}
	if len(wins) > 0 {
		s += "## Prior wins (LEARN FROM THESE)\n\n"
		for i, w := range wins {
			s += fmt.Sprintf("%d. class=%s Δ=%+.4g hash=%s\n", i+1, w.Class, w.Delta, shortHash(w.DiffHash))
		}
		s += "\n"
	}
	if len(losses) > 0 {
		s += "## Prior losses (DO NOT REPEAT)\n\n"
		for i, l := range losses {
			s += fmt.Sprintf("%d. class=%s reason=%s hash=%s\n", i+1, l.Class, l.FailReason, shortHash(l.DiffHash))
		}
		s += "\n"
	}
	s += "## Your task\n\nPropose a code change of class \"" + string(class) +
		"\" that improves the configured metric. Edit only allowlisted paths. " +
		"Do NOT delete or weaken tests, shorten benchmarks, or stub out functions. " +
		"Commit your changes on the current branch when done.\n"
	return s
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// Coordinator runs the autoresearch loop for a single repo. It Thompson-
// samples the next class, builds the prompt with memory-seed, dispatches
// up to `Parallel` ExperimentRunners concurrently, and updates priors.
//
// Termination: stops when ctx is cancelled or Stop() is called. Drains
// in-flight experiments before returning.
type Coordinator struct {
	Repo          string
	Bank          *HypothesisBank
	Sampler       *BayesSampler
	Runner        *ExperimentRunner
	PromptBuilder PromptBuilder
	ProgramMD     string
	Baseline      func() float64
	Parallel      int
	Budget        time.Duration

	stop chan struct{}
	once sync.Once
}

// NewCoordinator constructs a coordinator with sensible defaults.
func NewCoordinator(repo string, bank *HypothesisBank, sampler *BayesSampler, runner *ExperimentRunner, baseline func() float64, parallel int, budget time.Duration) *Coordinator {
	if parallel < 1 {
		parallel = 1
	}
	if budget <= 0 {
		budget = 5 * time.Minute
	}
	return &Coordinator{
		Repo:          repo,
		Bank:          bank,
		Sampler:       sampler,
		Runner:        runner,
		PromptBuilder: SimplePromptBuilder{},
		Baseline:      baseline,
		Parallel:      parallel,
		Budget:        budget,
		stop:          make(chan struct{}),
	}
}

// Stop signals the coordinator to drain and exit on its next tick.
func (c *Coordinator) Stop() {
	c.once.Do(func() { close(c.stop) })
}

// Run blocks until ctx is cancelled or Stop is called. Schedules
// experiments in waves of size Parallel.
func (c *Coordinator) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stop:
			return nil
		default:
		}

		// Run one wave of Parallel experiments concurrently.
		var wg sync.WaitGroup
		for i := 0; i < c.Parallel; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						c.emitPanic(fmt.Sprintf("%v", r))
					}
				}()
				if err := c.tick(ctx); err != nil {
					// One bad tick should not stop the whole loop.
					return
				}
			}()
		}
		wg.Wait()
	}
}

// tick dispatches one experiment.
func (c *Coordinator) tick(ctx context.Context) error {
	class := c.Sampler.Next(c.Repo)
	wins, _ := c.Bank.TopWins(c.Repo, 5)
	losses, _ := c.Bank.TopLosses(c.Repo, 5)
	prompt := c.PromptBuilder.Build(c.Repo, class, c.ProgramMD, wins, losses)
	p := Proposal{
		ID:               NewProposalID(),
		Repo:             c.Repo,
		Class:            class,
		Prompt:           prompt,
		PromptHash:       HashPrompt(prompt),
		ParentWinHashes:  hashes(wins),
		ParentLossHashes: hashes(losses),
	}
	c.Runner.emit(state.EventExperimentProposed, c.Repo, map[string]any{
		"id":                 p.ID,
		"repo":               p.Repo,
		"class":              string(p.Class),
		"prompt_hash":        p.PromptHash,
		"parent_win_hashes":  p.ParentWinHashes,
		"parent_loss_hashes": p.ParentLossHashes,
	})
	_, err := c.Runner.Run(ctx, p, c.Baseline(), c.Budget)
	return err
}

func (c *Coordinator) emitPanic(msg string) {
	if c.Runner == nil || c.Runner.Events == nil {
		return
	}
	evt := state.NewEvent(state.EventCoordinatorPanic, "autoresearch", "", map[string]any{
		"repo":  c.Repo,
		"error": msg,
	})
	_ = c.Runner.Events.Append(evt)
}

func hashes(exps []Experiment) []string {
	out := make([]string, 0, len(exps))
	for _, e := range exps {
		out = append(out, e.DiffHash)
	}
	return out
}
