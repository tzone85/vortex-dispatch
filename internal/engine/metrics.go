package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// PipelineMetrics holds aggregate performance data for the VXD pipeline.
type PipelineMetrics struct {
	// Requirement-level
	TotalRequirements   int
	CompletedRequirements int

	// Story-level
	TotalStories       int
	StoriesPassed      int // passed review + QA on first attempt
	StoriesEscalated   int
	StoriesMerged      int
	FirstPassRate      float64 // StoriesPassed / TotalStories

	// Phase timing (averages)
	AvgPlanningTime    time.Duration
	AvgExecutionTime   time.Duration
	AvgReviewTime      time.Duration
	AvgQATime          time.Duration
	AvgTotalTime       time.Duration // per story, end to end

	// Escalation breakdown
	EscalationsPerTier map[int]int // tier → count

	// Per-requirement breakdown
	RequirementStats   []RequirementStat
}

// RequirementStat holds metrics for a single requirement.
type RequirementStat struct {
	ReqID            string
	Title            string
	Status           string
	StoryCount       int
	MergedCount      int
	EscalationCount  int
	FirstPassRate    float64
	TotalDuration    time.Duration
}

// ComputeMetrics calculates pipeline metrics from the event store.
// Uses *SQLiteStore directly because ListRequirementsFiltered is not on the
// ProjectionStore interface.
func ComputeMetrics(es state.EventStore, ps *state.SQLiteStore, limit int) (PipelineMetrics, error) {
	m := PipelineMetrics{
		EscalationsPerTier: make(map[int]int),
	}

	// Get all requirements (most recent first)
	allReqs, err := ps.ListRequirementsFiltered(state.ReqFilter{})
	if err != nil {
		return m, fmt.Errorf("list requirements: %w", err)
	}

	// Sort by most recent first and limit
	sort.Slice(allReqs, func(i, j int) bool {
		return allReqs[i].CreatedAt.After(allReqs[j].CreatedAt)
	})
	if limit > 0 && len(allReqs) > limit {
		allReqs = allReqs[:limit]
	}

	m.TotalRequirements = len(allReqs)

	for _, req := range allReqs {
		if req.Status == "completed" {
			m.CompletedRequirements++
		}

		// Get stories for this requirement
		stories, err := ps.ListStories(state.StoryFilter{ReqID: req.ID})
		if err != nil {
			continue
		}

		reqStat := RequirementStat{
			ReqID:      req.ID,
			Title:      req.Title,
			Status:     req.Status,
			StoryCount: len(stories),
		}

		for _, story := range stories {
			m.TotalStories++

			if story.Status == "merged" || story.Status == "pr_submitted" {
				m.StoriesMerged++
				reqStat.MergedCount++
			}

			// Count escalations for this story
			escalations, _ := es.List(state.EventFilter{
				Type:    state.EventStoryEscalated,
				StoryID: story.ID,
			})
			if len(escalations) > 0 {
				m.StoriesEscalated++
				reqStat.EscalationCount += len(escalations)
				for _, esc := range escalations {
					payload := state.DecodePayload(esc.Payload)
					if toTier, ok := payload["to_tier"].(float64); ok {
						m.EscalationsPerTier[int(toTier)]++
					}
				}
			}

			// Check if story passed on first attempt (no REVIEW_FAILED or QA_FAILED)
			reviewFails, _ := es.List(state.EventFilter{
				Type:    state.EventStoryReviewFailed,
				StoryID: story.ID,
			})
			qaFails, _ := es.List(state.EventFilter{
				Type:    state.EventStoryQAFailed,
				StoryID: story.ID,
			})
			if len(reviewFails) == 0 && len(qaFails) == 0 && story.Status == "merged" {
				m.StoriesPassed++
			}

			// Calculate story duration (created → merged)
			storyEvents, _ := es.List(state.EventFilter{StoryID: story.ID})
			if len(storyEvents) >= 2 {
				duration := storyEvents[len(storyEvents)-1].Timestamp.Sub(storyEvents[0].Timestamp)
				m.AvgTotalTime += duration
				reqStat.TotalDuration += duration
			}
		}

		if reqStat.StoryCount > 0 {
			passed := 0
			for _, s := range stories {
				reviewFails, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: s.ID})
				qaFails, _ := es.List(state.EventFilter{Type: state.EventStoryQAFailed, StoryID: s.ID})
				if len(reviewFails) == 0 && len(qaFails) == 0 && s.Status == "merged" {
					passed++
				}
			}
			reqStat.FirstPassRate = float64(passed) / float64(reqStat.StoryCount) * 100
		}

		m.RequirementStats = append(m.RequirementStats, reqStat)
	}

	// Compute averages
	if m.TotalStories > 0 {
		m.FirstPassRate = float64(m.StoriesPassed) / float64(m.TotalStories) * 100
		m.AvgTotalTime = m.AvgTotalTime / time.Duration(m.TotalStories)
	}

	return m, nil
}

// FormatMetrics returns a human-readable summary of pipeline metrics.
func FormatMetrics(m PipelineMetrics) string {
	var b strings.Builder

	b.WriteString("=== VXD Pipeline Metrics ===\n\n")

	b.WriteString(fmt.Sprintf("Requirements:  %d total, %d completed\n", m.TotalRequirements, m.CompletedRequirements))
	b.WriteString(fmt.Sprintf("Stories:       %d total, %d merged, %d escalated\n", m.TotalStories, m.StoriesMerged, m.StoriesEscalated))
	b.WriteString(fmt.Sprintf("First-pass:    %.0f%% (stories that passed review+QA without retries)\n", m.FirstPassRate))
	b.WriteString(fmt.Sprintf("Avg time:      %s per story\n", formatDuration(m.AvgTotalTime)))

	if len(m.EscalationsPerTier) > 0 {
		b.WriteString("\nEscalations:\n")
		for tier := 1; tier <= 4; tier++ {
			if count, ok := m.EscalationsPerTier[tier]; ok {
				tierName := []string{"", "Senior", "Manager", "Tech Lead", "Paused"}
				name := "Unknown"
				if tier < len(tierName) {
					name = tierName[tier]
				}
				b.WriteString(fmt.Sprintf("  Tier %d (%s): %d\n", tier, name, count))
			}
		}
	}

	if len(m.RequirementStats) > 0 {
		b.WriteString("\nRecent Requirements:\n")
		for _, rs := range m.RequirementStats {
			title := rs.Title
			if len(title) > 50 {
				title = title[:50] + "..."
			}
			b.WriteString(fmt.Sprintf("  [%s] %s\n", rs.Status, title))
			b.WriteString(fmt.Sprintf("    Stories: %d | Merged: %d | First-pass: %.0f%% | Escalations: %d | Duration: %s\n",
				rs.StoryCount, rs.MergedCount, rs.FirstPassRate, rs.EscalationCount, formatDuration(rs.TotalDuration)))
		}
	}

	return b.String()
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
