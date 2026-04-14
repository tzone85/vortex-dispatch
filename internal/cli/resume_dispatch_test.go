package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// NOTE: Tests that dispatch real stories are intentionally excluded here
// because they spawn real tmux sessions and enter the monitor loop, causing
// timeouts. The resume early-exit paths are tested in resume_test.go.

// ---------------------------------------------------------------------------
// runAgents — test with actual agent data showing in output
// ---------------------------------------------------------------------------

func TestRunAgents_WithAgentData(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Seed a story first
	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-AGT",
		"title": "Agent Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-AGT", map[string]any{
		"id":         "STR-AGT",
		"req_id":     "REQ-AGT",
		"title":      "Agent Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Spawn an agent
	agentEvt := state.NewEvent(state.EventAgentSpawned, "agent-test-1", "STR-AGT", map[string]any{
		"agent_id":     "agent-test-1",
		"agent_type":   "junior",
		"session_name": "vxd-story-agt",
		"story_id":     "STR-AGT",
		"runtime":      "tmux",
	})
	s.Events.Append(agentEvt)
	s.Proj.Project(agentEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newAgentsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Error("expected some output")
	}
}

func TestRunAgents_FilterByInvalidStatus(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newAgentsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("status", "nonexistent_status")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "No agents with status") {
		t.Errorf("expected 'No agents with status', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runEscalations — with multiple escalation events
// ---------------------------------------------------------------------------

func TestRunEscalations_MultipleEscalations(t *testing.T) {
	dir, s := setupTestEnv(t)

	for i := 0; i < 3; i++ {
		sid := "STR-ESC" + string(rune('A'+i))
		escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", sid, map[string]any{
			"story_id":  sid,
			"from_tier": i,
			"to_tier":   i + 1,
			"reason":    "test failure #" + string(rune('0'+i)),
		})
		s.Events.Append(escEvt)
		s.Proj.Project(escEvt)
	}

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEscalationsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Escalations (3)") {
		t.Errorf("expected 'Escalations (3)', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// loadConfig — exercising all branches
// ---------------------------------------------------------------------------

func TestLoadConfig_ChainWithBothFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	repoContent := `workspace:
  state_dir: "~/.vxd"
models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-20250514
`
	repoCfgPath := filepath.Join(dir, "repo-config.yaml")
	os.WriteFile(repoCfgPath, []byte(repoContent), 0644)

	globalDir := filepath.Join(dir, ".vxd")
	os.MkdirAll(globalDir, 0o755)
	globalContent := `merge:
  base_branch: main
`
	os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)

	cfg, err := loadConfig(repoCfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.StateDir == "" {
		t.Error("expected non-empty state dir")
	}
}
