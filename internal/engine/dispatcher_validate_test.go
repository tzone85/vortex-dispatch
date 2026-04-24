package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newTestStoresForValidation(t *testing.T) (state.EventStore, state.ProjectionStore, func()) {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	cleanup := func() {
		es.Close()
		ps.Close()
	}
	return es, ps, cleanup
}

func TestDispatchWaveValidation(t *testing.T) {
	es, ps, cleanup := newTestStoresForValidation(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	dispatcher := engine.NewDispatcher(cfg, es, ps)

	// Helper function to create a basic DAG and stories
	createTestCase := func(storyID string) (*graph.DAG, []engine.PlannedStory, map[string]bool) {
		dag := graph.New()
		dag.AddNode(storyID)

		stories := []engine.PlannedStory{
			{
				ID:          storyID,
				Title:       "Test Story",
				Description: "Test description",
				Complexity:  1,
				WaveHint:    "parallel",
			},
		}

		completed := make(map[string]bool)

		return dag, stories, completed
	}

	// Test valid story IDs
	validIDs := []string{
		"s-001",
		"story.2",
		"my_story",
		"STORY-123",
		"test.story_001",
		"a1b2c3",
		"Story_With.Multiple-Parts",
		"123",
		"a",
		"A",
		"_",
		".",
		"-",
	}

	for _, id := range validIDs {
		t.Run("valid_"+id, func(t *testing.T) {
			dag, stories, completed := createTestCase(id)

			assignments, err := dispatcher.DispatchWave(dag, completed, "req-123", stories, 1)

			if err != nil {
				t.Errorf("Expected valid ID %q to pass validation, got error: %v", id, err)
			}

			if len(assignments) != 1 {
				t.Errorf("Expected 1 assignment for valid ID %q, got %d", id, len(assignments))
			}
		})
	}

	// Test invalid story IDs
	invalidIDs := []string{
		"s 001",           // space
		"story;rm",        // semicolon
		"../hack",         // slash
		"id`cmd`",         // backtick
		"story|rm",        // pipe
		"id&cmd",          // ampersand
		"story$var",       // dollar sign
		"id(cmd)",         // parentheses
		"story[0]",        // brackets
		"id{cmd}",         // braces
		"story*glob",      // asterisk
		"id?test",         // question mark
		"story<file",      // less than
		"id>file",         // greater than
		"story=value",     // equals
		"id+plus",         // plus
		"story%mod",       // percent
		"id@host",         // at sign
		"story#hash",      // hash
		"id!exclaim",      // exclamation
		"story~tilde",     // tilde
		"id^caret",        // caret
		"story\\escape",   // backslash
		"id\"quote",       // double quote
		"story'single",    // single quote
		"id,comma",        // comma
		"story:colon",     // colon
		"",                // empty string
	}

	for _, id := range invalidIDs {
		t.Run("invalid_"+id, func(t *testing.T) {
			dag, stories, completed := createTestCase(id)

			assignments, err := dispatcher.DispatchWave(dag, completed, "req-123", stories, 1)

			if err == nil {
				t.Errorf("Expected invalid ID %q to fail validation, but got no error", id)
			}

			if assignments != nil {
				t.Errorf("Expected nil assignments for invalid ID %q, got %v", id, assignments)
			}

			// Check that the error message is correct
			expectedPrefix := "unsafe story ID"
			if err != nil && len(err.Error()) > 0 && err.Error()[:len(expectedPrefix)] != expectedPrefix {
				t.Errorf("Expected error message to start with %q for invalid ID %q, got: %v", expectedPrefix, id, err.Error())
			}
		})
	}
}

func TestDispatchWaveValidation_MultipleStories(t *testing.T) {
	// Test case with multiple stories where one is invalid
	es, ps, cleanup := newTestStoresForValidation(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	dispatcher := engine.NewDispatcher(cfg, es, ps)

	dag := graph.New()
	dag.AddNode("valid-story")
	dag.AddNode("invalid story") // Contains space

	stories := []engine.PlannedStory{
		{
			ID:          "valid-story",
			Title:       "Valid Story",
			Description: "Valid story description",
			Complexity:  1,
			WaveHint:    "parallel",
		},
		{
			ID:          "invalid story", // Contains space
			Title:       "Invalid Story",
			Description: "Invalid story description",
			Complexity:  1,
			WaveHint:    "parallel",
		},
	}

	completed := make(map[string]bool)

	assignments, err := dispatcher.DispatchWave(dag, completed, "req-123", stories, 1)

	if err == nil {
		t.Error("Expected error for invalid story ID in batch, but got no error")
	}

	if assignments != nil {
		t.Errorf("Expected nil assignments for batch with invalid story ID, got %v", assignments)
	}
}