package agent

import (
	"math"
	"testing"
)

// TestOverallScore_MixedScores tests weighted scoring with realistic values.
func TestOverallScore_MixedScores(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   10,
		AvgQuality:     3.0,
		AvgReliability: 4.0,
		AvgDurationS:   1800, // 30 minutes
	}
	score := rep.OverallScore()
	// Quality: (3/5)*100*0.5 = 30
	// Reliability: (4/5)*100*0.3 = 24
	// Speed: (1 - 1800/3600)*100*0.2 = 10
	expected := 64.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected score %.2f, got %.2f", expected, score)
	}
}

// TestOverallScore_SlowAgent tests scoring when agent hits the speed cap.
func TestOverallScore_SlowAgent(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   3,
		AvgQuality:     5.0,
		AvgReliability: 5.0,
		AvgDurationS:   5000, // Exceeds 3600 cap
	}
	score := rep.OverallScore()
	// Quality: 50, Reliability: 30, Speed: 0 (capped at 3600, so 1-3600/3600=0)
	expected := 80.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected score %.2f for slow agent, got %.2f", expected, score)
	}
}

// TestOverallScore_AllZeroScores tests with all metrics at minimum non-zero.
func TestOverallScore_AllZeroScores(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   1,
		AvgQuality:     0.0,
		AvgReliability: 0.0,
		AvgDurationS:   3600, // Max cap
	}
	score := rep.OverallScore()
	// Quality: 0, Reliability: 0, Speed: 0
	expected := 0.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected score %.2f, got %.2f", expected, score)
	}
}

// TestOverallScore_SpeedNorm_ZeroDuration tests that zero duration gives max speed.
func TestOverallScore_SpeedNorm_ZeroDuration(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   1,
		AvgQuality:     0.0,
		AvgReliability: 0.0,
		AvgDurationS:   0, // Instant
	}
	score := rep.OverallScore()
	// Quality: 0, Reliability: 0, Speed: 100*0.2 = 20
	expected := 20.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected score %.2f for instant agent, got %.2f", expected, score)
	}
}

// TestOverallScore_ExactlyCap tests agent at exactly the 3600s boundary.
func TestOverallScore_ExactlyCap(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   2,
		AvgQuality:     5.0,
		AvgReliability: 5.0,
		AvgDurationS:   3600, // exactly at cap
	}
	score := rep.OverallScore()
	// Quality: 50, Reliability: 30, Speed: (1-3600/3600)*100*0.2 = 0
	expected := 80.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected score %.2f, got %.2f", expected, score)
	}
}

// TestComputeReputation_SingleScore tests with a single score.
func TestComputeReputation_SingleScore(t *testing.T) {
	scores := []Score{
		{AgentID: "sr-1", StoryID: "s-1", Quality: 5, Reliability: 3, DurationS: 600},
	}
	rep := ComputeReputation(scores)
	if rep.TotalStories != 1 {
		t.Fatalf("expected 1 story, got %d", rep.TotalStories)
	}
	if rep.AgentID != "sr-1" {
		t.Errorf("expected agent ID 'sr-1', got %s", rep.AgentID)
	}
	if rep.AvgQuality != 5.0 {
		t.Errorf("expected avg quality 5.0, got %f", rep.AvgQuality)
	}
	if rep.AvgReliability != 3.0 {
		t.Errorf("expected avg reliability 3.0, got %f", rep.AvgReliability)
	}
	if rep.AvgDurationS != 600.0 {
		t.Errorf("expected avg duration 600, got %f", rep.AvgDurationS)
	}
}

// TestComputeReputation_EmptySlice tests with an empty (non-nil) slice.
func TestComputeReputation_EmptySlice(t *testing.T) {
	rep := ComputeReputation([]Score{})
	if rep.TotalStories != 0 {
		t.Fatal("expected 0 stories for empty slice")
	}
	if rep.AgentID != "" {
		t.Errorf("expected empty agent ID, got %s", rep.AgentID)
	}
}

// TestOverallScore_HighQualityLowSpeed tests that quality is weighted higher than speed.
func TestOverallScore_HighQualityLowSpeed(t *testing.T) {
	rep := AgentReputation{
		TotalStories:   5,
		AvgQuality:     5.0,
		AvgReliability: 1.0,
		AvgDurationS:   3000,
	}
	score := rep.OverallScore()
	// Quality: (5/5)*100*0.5 = 50
	// Reliability: (1/5)*100*0.3 = 6
	// Speed: (1 - 3000/3600)*100*0.2 = 3.33
	expected := 59.33
	if math.Abs(score-expected) > 0.1 {
		t.Errorf("expected score ~%.2f, got %.2f", expected, score)
	}
}
