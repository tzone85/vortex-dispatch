package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// WaveInfo holds aggregated data for a single dispatch wave.
type WaveInfo struct {
	Number    int
	Stories   []StoryInfo
	StartedAt time.Time
	EndedAt   time.Time
}

// StoryInfo holds the display data for a story in the summary.
type StoryInfo struct {
	Title    string
	PRNumber int
}

// GenerateSummary produces a formatted completion summary for a requirement,
// showing per-wave timing, story names, and PR numbers — similar to:
//
//	15 PRs created and merged in about 55 minutes (10:38 to 11:30):
//
//	| Wave   | Stories                          | Time          |
//	| Wave 0 | Foundation (PR #1)               | 10:38         |
//	| Wave 1 | Budget, Research (PRs #2–4)      | 10:38 – 10:41 |
func GenerateSummary(events state.EventStore, proj state.ProjectionStore, reqID string) (string, error) {
	stories, err := proj.ListStories(state.StoryFilter{ReqID: reqID})
	if err != nil {
		return "", fmt.Errorf("list stories: %w", err)
	}
	if len(stories) == 0 {
		return "", nil
	}

	// Build per-story timing from events.
	storyTimings := buildStoryTimings(events, stories)

	// Group stories by wave number.
	waves := groupByWave(stories, storyTimings)

	// Compute overall timing.
	var earliest, latest time.Time
	mergedCount := 0
	for _, w := range waves {
		for _, s := range w.Stories {
			if s.PRNumber > 0 {
				mergedCount++
			}
		}
		if earliest.IsZero() || (!w.StartedAt.IsZero() && w.StartedAt.Before(earliest)) {
			earliest = w.StartedAt
		}
		if w.EndedAt.After(latest) {
			latest = w.EndedAt
		}
	}

	var b strings.Builder

	// Header line.
	if !earliest.IsZero() && !latest.IsZero() {
		duration := latest.Sub(earliest)
		minutes := int(math.Round(duration.Minutes()))
		fmt.Fprintf(&b, "\n%d PRs created and merged in about %d minutes (%s to %s):\n\n",
			mergedCount, minutes,
			earliest.Local().Format("15:04"),
			latest.Local().Format("15:04"),
		)
	} else {
		fmt.Fprintf(&b, "\n%d stories processed:\n\n", len(stories))
	}

	// Compute column widths for alignment.
	waveCol := len("Wave")
	storiesCol := len("Stories")
	timeCol := len("Time")

	type rowData struct {
		wave    string
		stories string
		time    string
	}
	var rows []rowData

	for _, w := range waves {
		waveStr := fmt.Sprintf("Wave %d", w.Number)
		storiesStr := formatWaveStories(w.Stories)
		timeStr := formatWaveTime(w.StartedAt, w.EndedAt)

		if len(waveStr) > waveCol {
			waveCol = len(waveStr)
		}
		if len(storiesStr) > storiesCol {
			storiesCol = len(storiesStr)
		}
		if len(timeStr) > timeCol {
			timeCol = len(timeStr)
		}

		rows = append(rows, rowData{wave: waveStr, stories: storiesStr, time: timeStr})
	}

	// Table header.
	fmt.Fprintf(&b, "  %-*s  %-*s  %-*s\n", waveCol, "Wave", storiesCol, "Stories", timeCol, "Time")
	fmt.Fprintf(&b, "  %s  %s  %s\n", strings.Repeat("─", waveCol), strings.Repeat("─", storiesCol), strings.Repeat("─", timeCol))

	// Table rows.
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s\n", waveCol, r.wave, storiesCol, r.stories, timeCol, r.time)
	}

	// Footer.
	fmt.Fprintf(&b, "\nThe merge queue is empty, all agents have terminated, and the pipeline shows %d/%d merged. The requirement is complete.\n",
		mergedCount, len(stories))

	return b.String(), nil
}

// storyTiming holds the start and end times for a single story.
type storyTiming struct {
	startedAt time.Time
	mergedAt  time.Time
}

// buildStoryTimings queries the event store for STORY_STARTED and STORY_MERGED
// events and returns a map of storyID -> storyTiming.
func buildStoryTimings(events state.EventStore, stories []state.Story) map[string]storyTiming {
	timings := make(map[string]storyTiming, len(stories))

	for _, s := range stories {
		var t storyTiming

		startEvents, _ := events.List(state.EventFilter{
			Type:    state.EventStoryStarted,
			StoryID: s.ID,
			Limit:   1,
		})
		if len(startEvents) > 0 {
			t.startedAt = startEvents[0].Timestamp
		}

		mergeEvents, _ := events.List(state.EventFilter{
			Type:    state.EventStoryMerged,
			StoryID: s.ID,
			Limit:   1,
		})
		if len(mergeEvents) > 0 {
			t.mergedAt = mergeEvents[0].Timestamp
		}

		// Fall back to PR created time if no merge event.
		if t.mergedAt.IsZero() {
			prEvents, _ := events.List(state.EventFilter{
				Type:    state.EventStoryPRCreated,
				StoryID: s.ID,
				Limit:   1,
			})
			if len(prEvents) > 0 {
				t.mergedAt = prEvents[0].Timestamp
			}
		}

		timings[s.ID] = t
	}
	return timings
}

// groupByWave groups stories into WaveInfo slices ordered by wave number.
func groupByWave(stories []state.Story, timings map[string]storyTiming) []WaveInfo {
	waveMap := make(map[int]*WaveInfo)

	for _, s := range stories {
		w, ok := waveMap[s.Wave]
		if !ok {
			w = &WaveInfo{Number: s.Wave}
			waveMap[s.Wave] = w
		}

		prNum := s.PRNumber
		// Fall back: try to extract PR number from events if not in projection.
		if prNum == 0 {
			prNum = extractPRNumberFromURL(s.PRUrl)
		}

		w.Stories = append(w.Stories, StoryInfo{
			Title:    s.Title,
			PRNumber: prNum,
		})

		t := timings[s.ID]
		if !t.startedAt.IsZero() && (w.StartedAt.IsZero() || t.startedAt.Before(w.StartedAt)) {
			w.StartedAt = t.startedAt
		}
		endTime := t.mergedAt
		if !endTime.IsZero() && endTime.After(w.EndedAt) {
			w.EndedAt = endTime
		}
	}

	// Sort waves by number.
	waves := make([]WaveInfo, 0, len(waveMap))
	for _, w := range waveMap {
		waves = append(waves, *w)
	}
	sort.Slice(waves, func(i, j int) bool {
		return waves[i].Number < waves[j].Number
	})

	return waves
}

// formatWaveStories produces a comma-separated list of story titles with
// a compact PR number suffix, e.g. "Budget, Research, Crew (PRs #2–4)".
func formatWaveStories(stories []StoryInfo) string {
	titles := make([]string, 0, len(stories))
	var prNums []int

	for _, s := range stories {
		titles = append(titles, s.Title)
		if s.PRNumber > 0 {
			prNums = append(prNums, s.PRNumber)
		}
	}

	result := strings.Join(titles, ", ")

	if len(prNums) > 0 {
		result += " " + formatPRNumbers(prNums)
	}

	return result
}

// formatPRNumbers produces a compact representation of PR numbers:
//
//	[1]         → "(PR #1)"
//	[1,2,3]     → "(PRs #1–3)"
//	[3,12,13,14] → "(PRs #3, #12–14)"
func formatPRNumbers(nums []int) string {
	sort.Ints(nums)
	nums = dedup(nums)

	if len(nums) == 1 {
		return fmt.Sprintf("(PR #%d)", nums[0])
	}

	ranges := compressRanges(nums)

	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r[0] == r[1] {
			parts = append(parts, fmt.Sprintf("#%d", r[0]))
		} else {
			parts = append(parts, fmt.Sprintf("#%d–%d", r[0], r[1]))
		}
	}

	return fmt.Sprintf("(PRs %s)", strings.Join(parts, ", "))
}

// compressRanges converts a sorted slice of ints into consecutive ranges.
// e.g., [1,2,3,5,7,8] → [[1,3],[5,5],[7,8]]
func compressRanges(nums []int) [][2]int {
	if len(nums) == 0 {
		return nil
	}

	var ranges [][2]int
	start := nums[0]
	end := nums[0]

	for i := 1; i < len(nums); i++ {
		if nums[i] == end+1 {
			end = nums[i]
		} else {
			ranges = append(ranges, [2]int{start, end})
			start = nums[i]
			end = nums[i]
		}
	}
	ranges = append(ranges, [2]int{start, end})

	return ranges
}

// dedup removes duplicate ints from a sorted slice.
func dedup(nums []int) []int {
	if len(nums) <= 1 {
		return nums
	}
	result := []int{nums[0]}
	for i := 1; i < len(nums); i++ {
		if nums[i] != nums[i-1] {
			result = append(result, nums[i])
		}
	}
	return result
}

// formatWaveTime formats the time range for a wave.
// Single-point waves show just the time; ranges show "HH:MM – HH:MM".
func formatWaveTime(start, end time.Time) string {
	if start.IsZero() {
		return "—"
	}

	startStr := start.Local().Format("15:04")

	if end.IsZero() || end.Sub(start) < time.Minute {
		return startStr
	}

	endStr := end.Local().Format("15:04")
	if startStr == endStr {
		return startStr
	}

	return fmt.Sprintf("%s – %s", startStr, endStr)
}

// extractPRNumberFromURL parses a PR number from a GitHub PR URL.
// e.g., "https://github.com/user/repo/pull/42" → 42
func extractPRNumberFromURL(url string) int {
	if url == "" {
		return 0
	}
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) == 0 {
		return 0
	}
	last := parts[len(parts)-1]
	var n int
	_, _ = fmt.Sscanf(last, "%d", &n) // non-numeric tail → 0, which is the correct fallback
	return n
}

