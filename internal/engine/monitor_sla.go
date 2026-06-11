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

	m.slaMu.Lock()
	defer m.slaMu.Unlock()

	// Already breached and emitted — skip
	if m.slaBreachedSet[storyID] {
		return
	}

	// Resolve start time (cache on first lookup).
	// Use the LATEST STORY_STARTED event so that resumed stories
	// measure SLA from the most recent attempt, not the original dispatch.
	startTime, ok := m.slaStartTimes[storyID]
	if !ok {
		events, err := m.eventStore.List(state.EventFilter{
			Type:    state.EventStoryStarted,
			StoryID: storyID,
		})
		if err != nil || len(events) == 0 {
			return // can't check without start time
		}
		startTime = events[len(events)-1].Timestamp
		m.slaStartTimes[storyID] = startTime
	}

	// Look up complexity from projection
	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		return
	}

	if !CheckSLA(m.config.SLA, story.Complexity, startTime) {
		return
	}

	// Breached — emit event once
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
		return
	}
	m.slaBreachedSet[storyID] = true

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
			if releaseErr := m.lifecycle.Release(context.Background(), ag.DB, outcome); releaseErr != nil {
				log.Printf("[monitor] SLA-breach devdb release failed for %s: %v (will GC later)", storyID, releaseErr)
			}
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
