package engine

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Run polls active agents at the configured interval until all are done
// or the context is cancelled. When all agents finish naturally, Run waits
// for their post-execution pipelines (review, QA, merge) to complete.
// If auto-resume is enabled (SetAutoResume was called), Run then dispatches
// the next wave of ready stories and continues monitoring. This repeats
// until all stories are complete or context is cancelled.
func (m *Monitor) Run(ctx context.Context, agents []ActiveAgent, repoDir string) error {
	return m.RunWithContext(ctx, agents, repoDir, nil)
}

// RunWithContext is like Run but accepts a RunContext for auto-resume.
func (m *Monitor) RunWithContext(ctx context.Context, agents []ActiveAgent, repoDir string, rc *RunContext) error {
	pollInterval := time.Duration(m.config.Monitor.PollIntervalMs) * time.Millisecond
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var pipelineWG sync.WaitGroup

	active := make(map[string]ActiveAgent, len(agents))
	for _, a := range agents {
		active[a.Assignment.SessionName] = a
	}

	log.Printf("[monitor] tracking %d agents, polling every %s", len(active), pollInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[monitor] context cancelled, %d agents still active", len(active))
			return nil // graceful detach, agents continue in tmux
		case <-ticker.C:
			if len(active) == 0 {
				log.Printf("[monitor] all agents finished, waiting for post-execution pipelines")
				pipelineWG.Wait()
				log.Printf("[monitor] all pipelines complete")

				// Auto-resume: dispatch next wave if possible.
				if rc != nil && m.dispatcher != nil && m.executor != nil {
					newAgents := m.dispatchNextWave(ctx, rc, repoDir)
					if len(newAgents) > 0 {
						for _, a := range newAgents {
							active[a.Assignment.SessionName] = a
						}
						log.Printf("[monitor] auto-resumed: tracking %d new agents", len(newAgents))
						continue
					}
				}

				return nil
			}

			m.pollOnce(ctx, &pipelineWG, active, repoDir)
		}
	}
}

// pollOnce performs a single pass over active agents, checking status and
// kicking off post-execution pipelines for any that have finished.
func (m *Monitor) pollOnce(ctx context.Context, wg *sync.WaitGroup, active map[string]ActiveAgent, repoDir string) {
	for sessionName, ag := range active {
		rt, err := m.registry.Get(ag.RuntimeName)
		if err != nil {
			continue
		}

		// Watchdog check (handles permission prompts, stuck detection)
		m.watchdog.Check(sessionName, rt)

		// SLA check (observational — emits STORY_SLA_BREACHED once if elapsed > max)
		m.checkSLA(ag)

		// Check if agent is done
		status, err := rt.DetectStatus(sessionName)
		if err != nil {
			log.Printf("[monitor] %s status check error: %v", ag.Assignment.StoryID, err)
			continue
		}

		log.Printf("[monitor] %s: %s", ag.Assignment.StoryID, status)

		if status != runtime.StatusDone && status != runtime.StatusTerminated {
			continue
		}

		log.Printf("[monitor] agent %s finished (status: %s)", ag.Assignment.AgentID, status)

		// Emit story completed
		completedEvt := state.NewEvent(
			state.EventStoryCompleted,
			ag.Assignment.AgentID,
			ag.Assignment.StoryID,
			map[string]any{
				"status": status.String(),
			},
		)
		if err := m.eventStore.Append(completedEvt); err != nil {
			log.Printf("[monitor] failed to append completed event for %s: %v", ag.Assignment.StoryID, err)
		}
		if err := m.projStore.Project(completedEvt); err != nil {
			log.Printf("[monitor] failed to project completed event for %s: %v", ag.Assignment.StoryID, err)
		}

		// Drive post-execution pipeline
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.postExecutionPipeline(ctx, ag, repoDir)
		}()

		// Remove from active tracking
		m.watchdog.ClearFingerprint(sessionName)
		delete(active, sessionName)
		m.clearSLATracking(ag.Assignment.StoryID)

		log.Printf("[monitor] %d agents remaining", len(active))
	}
}
