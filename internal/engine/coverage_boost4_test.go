package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func boost4EventStore(t *testing.T) *state.FileStore {
	t.Helper()
	dir := t.TempDir()
	fs, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

// --- WriteCheckpoint full coverage ---

func TestWriteCheckpoint_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cp.json")

	cp1 := Checkpoint{ReqID: "r1", Phase: PhaseDispatching, Timestamp: time.Now(), PID: os.Getpid()}
	if err := WriteCheckpoint(path, cp1); err != nil {
		t.Fatal(err)
	}

	cp2 := Checkpoint{ReqID: "r2", Phase: PhaseCompleted, Timestamp: time.Now(), PID: os.Getpid()}
	if err := WriteCheckpoint(path, cp2); err != nil {
		t.Fatal(err)
	}

	read, err := ReadCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if read.ReqID != "r2" {
		t.Errorf("expected r2, got %q", read.ReqID)
	}
}

func TestClearCheckpoint_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cp.json")

	cp := Checkpoint{ReqID: "r1", Phase: PhaseDispatching, Timestamp: time.Now(), PID: os.Getpid()}
	WriteCheckpoint(path, cp)

	ClearCheckpoint(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected checkpoint file to be removed")
	}
}

func TestClearCheckpoint_NonexistentFile(t *testing.T) {
	// Should not panic
	ClearCheckpoint("/nonexistent/path/cp.json")
}

// --- RetryCountAtCurrentTier after escalation ---

func TestRetryCountAtCurrentTier_AfterEscalation(t *testing.T) {
	es := boost4EventStore(t)
	cfg := config.RoutingConfig{MaxRetriesBeforeEscalation: 2, MaxSeniorRetries: 2}
	em := NewEscalationMachine(es, cfg)

	// Add failures before escalation
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "rev", "s1", nil))
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "rev", "s1", nil))

	// Escalate
	es.Append(state.NewEvent(state.EventStoryEscalated, "mon", "s1",
		map[string]any{"from_tier": 0, "to_tier": float64(1)}))

	// Add one failure after escalation
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s1", nil))

	count, err := em.RetryCountAtCurrentTier("s1")
	if err != nil {
		t.Fatal(err)
	}
	// Should only count the 1 failure after escalation, not the 2 before
	if count != 1 {
		t.Errorf("expected 1 (only post-escalation), got %d", count)
	}
}

// --- CurrentTier with multiple escalations ---

func TestCurrentTier_ThreeEscalations(t *testing.T) {
	es := boost4EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	es.Append(state.NewEvent(state.EventStoryEscalated, "mon", "s1",
		map[string]any{"from_tier": 0, "to_tier": float64(1)}))
	es.Append(state.NewEvent(state.EventStoryEscalated, "mon", "s1",
		map[string]any{"from_tier": 1, "to_tier": float64(2)}))
	es.Append(state.NewEvent(state.EventStoryEscalated, "mon", "s1",
		map[string]any{"from_tier": 2, "to_tier": float64(3)}))

	tier, err := em.CurrentTier("s1")
	if err != nil {
		t.Fatal(err)
	}
	if tier != 3 {
		t.Errorf("expected tier 3, got %d", tier)
	}
}

// --- detectExistingCodebase ---

func TestDetectExistingCodebase_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	// Not a git repo, so should return false
	if detectExistingCodebase(dir) {
		t.Error("expected false for non-git directory")
	}
}

func TestDetectExistingCodebase_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if detectExistingCodebase(dir) {
		t.Error("expected false for empty directory")
	}
}

// --- SanitizeProjectName ---

func TestSanitizeProjectName_SpecialChars(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Project!!", "my-project"},
		{"test@repo#name", "test-repo-name"},
		{"UPPER-case.git", "upper-case"},
		{"---leading---trailing---", "leading-trailing"},
		{"!@#$%^&*()", "unnamed"},
	}
	for _, tc := range tests {
		got := SanitizeProjectName(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeProjectName(%q): expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

// --- ResolveMode with config priority ---

func TestResolveMode_ConfigTakesPrecedence(t *testing.T) {
	es := boost4EventStore(t)
	gate := NewReviewGate(es)

	mode := gate.ResolveMode("req-1", config.MergeConfig{ReviewMode: "plan_only"})
	if mode != "plan_only" {
		t.Errorf("expected plan_only from config, got %q", mode)
	}
}

// --- ReleaseLock ---

func TestReleaseLock_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vxd.lock")

	AcquireLock(lockPath, "req-1")
	ReleaseLock(lockPath)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed")
	}
}

func TestReleaseLock_NonexistentFile(t *testing.T) {
	// Should not panic
	ReleaseLock("/nonexistent/path/lock.json")
}

// --- AcquireLock stale detection ---

func TestAcquireLock_StaleReclaim(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vxd.lock")

	// Write a lock with a dead PID
	writeLockFile(lockPath, LockInfo{
		PID:       9999999, // very likely doesn't exist
		ReqID:     "req-old",
		StartedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	// Should reclaim stale lock
	info, err := AcquireLock(lockPath, "req-new")
	if err != nil {
		t.Fatalf("expected stale lock reclaim, got error: %v", err)
	}
	if info.ReqID != "req-new" {
		t.Errorf("expected req-new, got %q", info.ReqID)
	}
}

// --- buildEffort ---

func TestBuildEffort_EmptyStories(t *testing.T) {
	rb := &ReportBuilder{cfg: config.Config{Billing: config.DefaultConfig().Billing}}
	est := rb.buildEffort(nil)
	if est.Summary.StoryCount != 0 {
		t.Errorf("expected 0 stories, got %d", est.Summary.StoryCount)
	}
}
