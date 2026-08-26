package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// setupReplayEnv creates a bare VXD project state directory (no git repo
// needed — replay resolves the project via the explicit --project flag).
func setupReplayEnv(t *testing.T) (dir, projectDir string) {
	t.Helper()
	dir = t.TempDir()
	projectDir = filepath.Join(dir, ".vxd", "projects", "test-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	return dir, projectDir
}

// newReplayTestCmd builds the replay command wired to the temp workspace:
// config points at a nonexistent file (falls back to defaults whose
// state_dir ~/.vxd resolves under HOME=tempdir), project is pinned.
func newReplayTestCmd(t *testing.T, dir string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := newReplayCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent-vxd.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	t.Setenv("HOME", dir)
	return cmd, &buf
}

// seedReplayEvents appends the four lifecycle events from the spec to
// events.jsonl AND projects them, returning the pre-delete projection state.
func seedReplayEvents(t *testing.T, projectDir string) (state.Story, state.Requirement) {
	t.Helper()
	es, err := state.NewFileStore(filepath.Join(projectDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		es.Close()
		t.Fatalf("create sqlite store: %v", err)
	}

	events := []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
			"id":          "REQ00001",
			"title":       "Replay Req",
			"description": "seeded for replay test",
		}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR001", map[string]any{
			"id":         "STR001",
			"req_id":     "REQ00001",
			"title":      "Story One",
			"complexity": 3,
		}),
		state.NewEvent(state.EventStoryStarted, "", "STR001", nil),
		state.NewEvent(state.EventStoryCompleted, "", "STR001", nil),
	}
	for _, evt := range events {
		if err := es.Append(evt); err != nil {
			t.Fatalf("append event: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project event: %v", err)
		}
	}

	preStory, err := ps.GetStory("STR001")
	if err != nil {
		t.Fatalf("get pre-delete story: %v", err)
	}
	preReq, err := ps.GetRequirement("REQ00001")
	if err != nil {
		t.Fatalf("get pre-delete requirement: %v", err)
	}
	// Fixture sanity: the seeded sequence must end at story=review, req=pending.
	if preStory.Status != "review" {
		t.Fatalf("fixture drift: expected pre-delete story status 'review', got %q", preStory.Status)
	}
	if preReq.Status != "pending" {
		t.Fatalf("fixture drift: expected pre-delete req status 'pending', got %q", preReq.Status)
	}

	if err := es.Close(); err != nil {
		t.Fatalf("close event store: %v", err)
	}
	if err := ps.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	return preStory, preReq
}

func removeDBFiles(t *testing.T, projectDir string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(projectDir, "vxd.db"+suffix)); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove vxd.db%s: %v", suffix, err)
		}
	}
}

// assertProjectionMatchesPreDelete reopens the rebuilt store and verifies the
// requirement + story statuses equal the state captured before deletion.
func assertProjectionMatchesPreDelete(t *testing.T, projectDir string, preStory state.Story, preReq state.Requirement) {
	t.Helper()
	ps, err := state.NewSQLiteStore(filepath.Join(projectDir, "vxd.db"))
	if err != nil {
		t.Fatalf("reopen rebuilt store: %v", err)
	}
	defer ps.Close()

	story, err := ps.GetStory("STR001")
	if err != nil {
		t.Fatalf("get rebuilt story: %v", err)
	}
	if story.Status != preStory.Status {
		t.Errorf("story status = %q, want %q (pre-delete)", story.Status, preStory.Status)
	}
	req, err := ps.GetRequirement("REQ00001")
	if err != nil {
		t.Fatalf("get rebuilt requirement: %v", err)
	}
	if req.Status != preReq.Status {
		t.Errorf("requirement status = %q, want %q (pre-delete)", req.Status, preReq.Status)
	}
}

func TestNewReplayCmd(t *testing.T) {
	cmd := newReplayCmd()
	if cmd.Use != "replay" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("flag 'dry-run' not registered")
	}
}

// TestReplay_WiredIntoRoot proves the feature is ACTIVATED, not just
// implemented: the command must be registered on rootCmd.
func TestReplay_WiredIntoRoot(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "replay" {
			return
		}
	}
	t.Fatal("replay command not registered on rootCmd — add rootCmd.AddCommand(newReplayCmd()) to internal/cli/root.go")
}

// TestReplay_RebuildsProjections covers the spec's disaster-recovery case:
// the SQLite projection is DELETED, then replay rebuilds it from the event
// log. No backup can exist here — there was no database left to move aside —
// so the absence of vxd.db.bak-* is itself asserted.
func TestReplay_RebuildsProjections(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	preStory, preReq := seedReplayEvents(t, projectDir)

	// Simulate corruption/loss: the SQLite projection is gone.
	removeDBFiles(t, projectDir)

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Replayed 4 events") {
		t.Errorf("expected 'Replayed 4 events' in output, got: %s", output)
	}
	if !strings.Contains(output, "REQ_SUBMITTED") || !strings.Contains(output, "STORY_COMPLETED") {
		t.Errorf("expected per-type tally in output, got: %s", output)
	}

	assertProjectionMatchesPreDelete(t, projectDir, preStory, preReq)

	// Nothing existed to back up — replay must not fabricate one.
	baks, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*"))
	if len(baks) != 0 {
		t.Errorf("expected no backup when the db was deleted before replay, got %v", baks)
	}
}

// TestReplay_BackupsExistingDB covers the other recovery shape: the old
// projection still exists (corrupt/divergent rather than deleted). Replay
// must move it aside to vxd.db.bak-<timestamp> (never destroy it) and still
// produce a fresh projection matching the event log.
func TestReplay_BackupsExistingDB(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	preStory, preReq := seedReplayEvents(t, projectDir)

	// Deliberately leave vxd.db in place.

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if !strings.Contains(buf.String(), "backed up") {
		t.Errorf("expected backup path reported in output, got: %s", buf.String())
	}

	assertProjectionMatchesPreDelete(t, projectDir, preStory, preReq)

	baks, _ := filepath.Glob(filepath.Join(projectDir, "vxd.db.bak-*"))
	if len(baks) != 1 {
		t.Fatalf("expected exactly one vxd.db.bak-<timestamp> backup, got %d (%v)", len(baks), baks)
	}
	info, err := os.Stat(baks[0])
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty — old db was not preserved")
	}
}

func TestReplay_DryRunReportsCorruptLines(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)
	_, _ = seedReplayEvents(t, projectDir) // 4 valid lines

	// Inject a garbage line — it lands on line 5.
	f, err := os.OpenFile(filepath.Join(projectDir, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	if _, err := f.WriteString("this is definitely not json\n"); err != nil {
		t.Fatalf("append garbage line: %v", err)
	}
	f.Close()

	// Snapshot the db so we can prove dry-run left it untouched.
	dbPath := filepath.Join(projectDir, "vxd.db")
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db before dry-run: %v", err)
	}

	cmd, buf := newReplayTestCmd(t, dir)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected non-zero exit for corrupt event log")
	}
	if !strings.Contains(buf.String(), "line 5") {
		t.Errorf("output should report the corrupt line number, got: %s", buf.String())
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db after dry-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("dry-run modified the SQLite database")
	}
}

func TestReplay_RefusesWhenLocked(t *testing.T) {
	dir, projectDir := setupReplayEnv(t)

	// Simulate a LIVE pipeline: a lock file owned by this (alive) process.
	lockInfo := engine.LockInfo{
		PID:       os.Getpid(),
		ReqID:     "REQ-LIVE-42",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(lockInfo)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	lockPath := filepath.Join(projectDir, "vxd.lock")
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	cmd, _ := newReplayTestCmd(t, dir)
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected refusal while a live pipeline holds the lock")
	}
	if !strings.Contains(err.Error(), "REQ-LIVE-42") {
		t.Errorf("error should mention the running requirement, got: %v", err)
	}
}
