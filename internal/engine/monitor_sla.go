package engine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// clearSLATracking removes per-story SLA state. Called when a story finishes
// (success, failure, or terminated) to prevent unbounded map growth in
// long-running monitor sessions.
func (m *Monitor) clearSLATracking(storyID string) {
	m.slaMu.Lock()
	defer m.slaMu.Unlock()
	delete(m.slaStartTimes, storyID)
	delete(m.slaBreachedSet, storyID)
}

// checkSLA checks if the given active agent's story has breached its SLA.
// On first call for a story, looks up start time from the event log and
// complexity from the projection store (cached in slaStartTimes).
// Emits STORY_SLA_BREACHED at most once per story.
func (m *Monitor) checkSLA(ag ActiveAgent) {
	storyID := ag.Assignment.StoryID

	// Fast path: skip if this story's breach was already handled. The lock
	// guards ONLY the in-memory slaStartTimes/slaBreachedSet maps — never
	// store reads or network calls, which used to be held under it and could
	// stall the entire polling loop behind a slow event-store or devdb call.
	m.slaMu.Lock()
	alreadyBreached := m.slaBreachedSet[storyID]
	startTime, cached := m.slaStartTimes[storyID]
	m.slaMu.Unlock()
	if alreadyBreached {
		return
	}

	// Resolve start time (cache on first lookup) WITHOUT holding the lock.
	// Use the LATEST STORY_STARTED event so that resumed stories
	// measure SLA from the most recent attempt, not the original dispatch.
	if !cached {
		events, err := m.eventStore.List(state.EventFilter{
			Type:    state.EventStoryStarted,
			StoryID: storyID,
		})
		if err != nil || len(events) == 0 {
			return // can't check without start time
		}
		startTime = events[len(events)-1].Timestamp
		m.slaMu.Lock()
		m.slaStartTimes[storyID] = startTime
		m.slaMu.Unlock()
	}

	// Look up complexity from projection (no lock held).
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		return
	}

	if !CheckSLA(m.config.SLA, story.Complexity, startTime) {
		return
	}

	// Claim the breach exactly once. A second poll (or a concurrent caller)
	// that loses the race sees the flag already set and bails.
	m.slaMu.Lock()
	if m.slaBreachedSet[storyID] {
		m.slaMu.Unlock()
		return
	}
	m.slaBreachedSet[storyID] = true
	m.slaMu.Unlock()

	// All I/O below runs WITHOUT the lock held.
	maxDur := MaxDurationFor(m.config.SLA, story.Complexity)
	elapsed := time.Since(startTime)
	log.Printf("[monitor] SLA BREACH: %s elapsed=%v max=%v complexity=%d",
		storyID, elapsed.Round(time.Second), maxDur, story.Complexity)

	evt := state.NewEvent(state.EventStorySLABreached, ag.Assignment.AgentID, storyID, map[string]any{
		"complexity":      story.Complexity,
		"started_at":      startTime,
		"elapsed_seconds": int(elapsed.Seconds()),
		"max_minutes":     int(maxDur.Minutes()),
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[monitor] append SLA breach event for %s: %v", storyID, err)
		// Release the claim so a later poll can retry the emit.
		m.slaMu.Lock()
		delete(m.slaBreachedSet, storyID)
		m.slaMu.Unlock()
		return
	}

	// Optional webhook notification — fire-and-forget, errors logged not surfaced.
	// Bound the call with a 10s ctx so a hung webhook doesn't leak the
	// goroutine through monitor shutdown. context.Background() with no
	// timeout used to keep these goroutines alive indefinitely (multiplied
	// across SLA-breached stories), preventing clean process exit.
	if m.notifier != nil {
		go func() {
			nctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			notifyErr := m.notifier.Notify(nctx, notify.Message{
				Title:    fmt.Sprintf("SLA breach: %s", storyID),
				Body:     fmt.Sprintf("Story exceeded its %v SLA", maxDur),
				Severity: "warn",
				Fields: map[string]string{
					"story_id":   storyID,
					"complexity": fmt.Sprintf("%d", story.Complexity),
					"elapsed":    elapsed.Round(time.Second).String(),
					"max":        maxDur.String(),
				},
			})
			if notifyErr != nil {
				log.Printf("[monitor] notify SLA breach for %s: %v", storyID, notifyErr)
			}
		}()
	}

	// Optional auto-escalation (opt-in via config)
	if m.config.SLA.AutoEscalate {
		// Release current attempt's devdb before tier escalation.
		// The next attempt will provision a fresh one; without this, orphans
		// accumulate within a single requirement run.
		if m.lifecycle != nil && ag.DB.ID != "" {
			outcome := devdb.OutcomeFailed
			// Bound the teardown so a hung devdb provider can't block the
			// polling goroutine or survive monitor shutdown indefinitely.
			rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if releaseErr := m.lifecycle.Release(rctx, ag.DB, outcome); releaseErr != nil {
				log.Printf("[monitor] SLA-breach devdb release failed for %s: %v (will GC later)", storyID, releaseErr)
			}
			cancel()
		}
		m.escalateOnSLABreach(storyID, ag.Assignment.AgentID, elapsed)
	}
}

// escalateOnSLABreach triggers tier escalation due to SLA breach when
// sla.auto_escalate is enabled. Caps at tier 3 (does not pause via SLA).
func (m *Monitor) escalateOnSLABreach(storyID, fromAgent string, elapsed time.Duration) {
	currentTier, err := m.escalation.CurrentTier(storyID)
	if err != nil {
		log.Printf("[monitor] SLA escalate: get current tier for %s: %v", storyID, err)
		return
	}
	nextTier := currentTier + 1
	if nextTier >= 4 {
		log.Printf("[monitor] SLA escalate: %s already at tier %d, not escalating to pause", storyID, currentTier)
		return
	}

	reason := fmt.Sprintf("SLA breach (elapsed %s) — auto-escalated to tier %d",
		elapsed.Round(time.Second), nextTier)
	log.Printf("[monitor] auto-escalating %s from tier %d to tier %d: %s",
		storyID, currentTier, nextTier, reason)

	escEvt := state.NewEvent(state.EventStoryEscalated, fromAgent, storyID, map[string]any{
		"from_tier": currentTier,
		"to_tier":   nextTier,
		"reason":    reason,
	})
	if err := m.eventStore.Append(escEvt); err != nil {
		log.Printf("[monitor] append SLA escalation: %v", err)
		return
	}
	if err := m.projStore.Project(escEvt); err != nil {
		log.Printf("[monitor] project SLA escalation: %v", err)
	}

	// Reset to draft so dispatcher picks it up at the new tier
	resetEvt := state.NewEvent(state.EventStoryReviewFailed, fromAgent, storyID, map[string]any{
		"reason": reason,
	})
	if err := m.eventStore.Append(resetEvt); err != nil {
		log.Printf("[monitor] append SLA reset: %v", err)
		return
	}
	if err := m.projStore.Project(resetEvt); err != nil {
		log.Printf("[monitor] project SLA reset: %v", err)
	}
}
