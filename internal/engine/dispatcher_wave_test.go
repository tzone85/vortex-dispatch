package engine

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestAutoTagWaveHints_NoPatterns(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: nil,
			},
		},
	}
	stories := []PlannedStory{
		{ID: "s-001", OwnedFiles: []string{"main.go"}},
		{ID: "s-002", OwnedFiles: []string{"handler.go"}},
	}
	d.autoTagWaveHints(stories)

	for _, s := range stories {
		if s.WaveHint != "parallel" {
			t.Errorf("story %s expected parallel, got %s", s.ID, s.WaveHint)
		}
	}
}

func TestAutoTagWaveHints_SequentialFile(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod", "go.sum"},
			},
		},
	}
	stories := []PlannedStory{
		{ID: "s-001", OwnedFiles: []string{"go.mod", "main.go"}},
		{ID: "s-002", OwnedFiles: []string{"handler.go"}},
	}
	d.autoTagWaveHints(stories)

	if stories[0].WaveHint != "sequential" {
		t.Errorf("s-001 owns go.mod, expected sequential, got %s", stories[0].WaveHint)
	}
	if stories[1].WaveHint != "parallel" {
		t.Errorf("s-002 expected parallel, got %s", stories[1].WaveHint)
	}
}

func TestAutoTagWaveHints_PreExistingHint(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{
			Planning: config.PlanningConfig{
				SequentialFilePatterns: []string{"go.mod"},
			},
		},
	}
	stories := []PlannedStory{
		{ID: "s-001", OwnedFiles: []string{"go.mod"}, WaveHint: "parallel"},
	}
	d.autoTagWaveHints(stories)

	// Should not override pre-existing hint
	if stories[0].WaveHint != "parallel" {
		t.Errorf("expected pre-existing hint to be preserved, got %s", stories[0].WaveHint)
	}
}

func TestSelectDispatchable_SequentialFirst(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{},
	}
	stories := []PlannedStory{
		{ID: "s-001", WaveHint: "parallel"},
		{ID: "s-002", WaveHint: "sequential"},
		{ID: "s-003", WaveHint: "parallel"},
	}
	got := d.selectDispatchable(stories)
	if len(got) != 1 {
		t.Fatalf("expected 1 story (sequential only), got %d", len(got))
	}
	if got[0].ID != "s-002" {
		t.Errorf("expected sequential story s-002, got %s", got[0].ID)
	}
}

func TestSelectDispatchable_AllParallel(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{},
	}
	stories := []PlannedStory{
		{ID: "s-001", WaveHint: "parallel", OwnedFiles: []string{"a.go"}},
		{ID: "s-002", WaveHint: "parallel", OwnedFiles: []string{"b.go"}},
	}
	got := d.selectDispatchable(stories)
	if len(got) != 2 {
		t.Errorf("expected 2 parallel stories, got %d", len(got))
	}
}

func TestSelectDispatchable_Empty(t *testing.T) {
	d := &Dispatcher{
		config: config.Config{},
	}
	got := d.selectDispatchable(nil)
	if len(got) != 0 {
		t.Errorf("expected 0 stories, got %d", len(got))
	}
}
