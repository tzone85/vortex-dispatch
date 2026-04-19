package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func boost3EventStore(t *testing.T) *state.FileStore {
	t.Helper()
	dir := t.TempDir()
	fs, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

// --- WriteCheckpoint success path ---

func TestWriteCheckpoint_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	cp := Checkpoint{
		ReqID:      "req-1",
		Phase:      PhaseMonitoring,
		WaveNumber: 2,
		ActiveAgents: []CheckpointAgent{
			{StoryID: "s1", SessionName: "vxd-s1", WorktreePath: "/tmp/wt-s1"},
		},
		MergingStory: "s2",
		Timestamp:    time.Now(),
		PID:          os.Getpid(),
	}
	if err := WriteCheckpoint(path, cp); err != nil {
		t.Fatal(err)
	}

	read, err := ReadCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if read.ReqID != "req-1" {
		t.Errorf("expected req-1, got %q", read.ReqID)
	}
	if read.Phase != PhaseMonitoring {
		t.Errorf("expected monitoring, got %q", read.Phase)
	}
	if read.WaveNumber != 2 {
		t.Errorf("expected wave 2, got %d", read.WaveNumber)
	}
	if len(read.ActiveAgents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(read.ActiveAgents))
	}
	if read.ActiveAgents[0].StoryID != "s1" {
		t.Errorf("expected s1, got %q", read.ActiveAgents[0].StoryID)
	}
}

// --- extractRepoName additional paths ---

func TestExtractRepoName_HTTPSWithGitSuffix(t *testing.T) {
	got := extractRepoName("https://github.com/myorg/my-repo.git")
	if got != "my-repo" {
		t.Errorf("expected my-repo, got %q", got)
	}
}

func TestExtractRepoName_JustRepoName(t *testing.T) {
	got := extractRepoName("simple-repo")
	if got != "simple-repo" {
		t.Errorf("expected simple-repo, got %q", got)
	}
}

func TestExtractRepoName_SSHStyleNoOrg(t *testing.T) {
	got := extractRepoName("git@github.com:repo-only.git")
	if got != "repo-only" {
		t.Errorf("expected repo-only, got %q", got)
	}
}

// --- ShouldEscalate after escalation ---

func TestShouldEscalate_AtTier1(t *testing.T) {
	es := boost3EventStore(t)
	cfg := config.RoutingConfig{
		MaxRetriesBeforeEscalation: 2,
		MaxSeniorRetries:           1,
		MaxManagerAttempts:         1,
	}
	em := NewEscalationMachine(es, cfg)

	// Escalate to tier 1
	es.Append(state.NewEvent(state.EventStoryEscalated, "monitor", "s1",
		map[string]any{"from_tier": 0, "to_tier": float64(1)}))

	// Add one failure at tier 1
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s1", nil))

	shouldEsc, tier, err := em.ShouldEscalate("s1")
	if err != nil {
		t.Fatal(err)
	}
	if !shouldEsc {
		t.Error("should escalate at tier 1 with 1 failure (max 1)")
	}
	if tier != 2 {
		t.Errorf("expected tier 2, got %d", tier)
	}
}

// --- ReviewGate PendingApprovals ---

func TestReviewGate_PendingApprovals_Empty(t *testing.T) {
	es := boost3EventStore(t)
	gate := NewReviewGate(es)

	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	pending, err := gate.PendingApprovals("req-1", ps)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestReviewGate_PendingApprovals_WithStories(t *testing.T) {
	es := boost3EventStore(t)
	gate := NewReviewGate(es)

	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// Seed a story in awaiting_approval status
	evt := state.NewEvent(state.EventStoryCreated, "tl", "s1", map[string]any{
		"id": "s1", "req_id": "req-1", "title": "Test Story",
		"complexity": 3,
	})
	es.Append(evt)
	ps.Project(evt)

	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", "s1", map[string]any{
		"reason": "review required",
	})
	es.Append(awaitEvt)
	ps.Project(awaitEvt)

	pending, err := gate.PendingApprovals("req-1", ps)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}

// --- ResolveMode coverage ---

func TestResolveMode_FromEvent(t *testing.T) {
	es := boost3EventStore(t)
	gate := NewReviewGate(es)

	// Emit a review mode event
	es.Append(state.NewEvent(state.EventReviewModeSet, "cli", "", map[string]any{
		"req_id": "req-1",
		"mode":   "manual",
	}))

	mode := gate.ResolveMode("req-1", config.MergeConfig{ReviewMode: "auto"})
	if mode != "manual" {
		t.Errorf("expected manual (from event), got %q", mode)
	}
}

func TestResolveMode_FallbackToAutoMerge(t *testing.T) {
	es := boost3EventStore(t)
	gate := NewReviewGate(es)

	mode := gate.ResolveMode("req-1", config.MergeConfig{AutoMerge: true})
	if mode != "auto" {
		t.Errorf("expected auto from AutoMerge, got %q", mode)
	}
}

func TestResolveMode_FallbackToManual(t *testing.T) {
	es := boost3EventStore(t)
	gate := NewReviewGate(es)

	mode := gate.ResolveMode("req-1", config.MergeConfig{})
	if mode != "manual" {
		t.Errorf("expected manual fallback, got %q", mode)
	}
}

// --- MigrateOldLayout success path ---

func TestMigrateOldLayout_Migrates(t *testing.T) {
	dir := t.TempDir()

	// Create old layout sentinel file
	os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte("line1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "vxd.db"), []byte("db"), 0o644)
	os.MkdirAll(filepath.Join(dir, "worktrees"), 0o755)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o755)

	migrated, err := MigrateOldLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Error("expected migration to happen")
	}

	// Verify files were moved
	if _, err := os.Stat(filepath.Join(dir, "events.jsonl")); !os.IsNotExist(err) {
		t.Error("old events.jsonl should have been moved")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "_legacy", "events.jsonl")); err != nil {
		t.Error("events.jsonl should exist in legacy dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "_legacy", "metadata.json")); err != nil {
		t.Error("metadata.json should exist in legacy dir")
	}
}

// --- SetDryRun ---

func TestMonitor_SetDryRun(t *testing.T) {
	m := &Monitor{}
	m.SetDryRun(true)
	if !m.dryRun {
		t.Error("expected dryRun to be true")
	}
}

// --- RenderHTML coverage ---

func TestRenderHTML_ClientReport(t *testing.T) {
	data := ReportData{
		Title:         "HTML Test",
		RequirementID: "r1",
		Description:   "Test description <script>alert('xss')</script>",
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{Title: "S1", Status: "merged", Complexity: 3, PRNumber: 5, PRUrl: "https://github.com/org/repo/pull/5"},
		},
		Effort:   Estimate{Summary: EstimateSummary{Currency: "USD", StoryCount: 1}},
		Timeline: []TimelineEntry{{Timestamp: time.Now(), EventType: "test", Description: "event"}},
	}
	html := RenderHTML(data, "proj", false)
	if !strings.Contains(html, "<html") {
		t.Error("expected HTML output")
	}
	// XSS should be escaped
	if strings.Contains(html, "<script>") {
		t.Error("expected XSS to be escaped")
	}
}

func TestRenderHTML_InternalReport(t *testing.T) {
	data := ReportData{
		Title:         "HTML Internal",
		RequirementID: "r2",
		Description:   "Internal desc",
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{
				Title:      "S1",
				Status:     "merged",
				Complexity: 2,
				Attempts: []Attempt{
					{Number: 1, Role: "junior", Outcome: "failed", Error: "build err"},
					{Number: 2, Role: "senior", Outcome: "success"},
				},
			},
		},
		Effort:     Estimate{Summary: EstimateSummary{Currency: "USD", StoryCount: 1}},
		Timeline:   []TimelineEntry{},
		AgentStats: []AgentStat{{AgentID: "a1", StoriesWorked: 2, Escalations: 1}},
	}
	html := RenderHTML(data, "proj", true)
	if !strings.Contains(html, "Agent Performance") {
		t.Error("expected internal section in HTML")
	}
}

// --- FormatDuration edge cases ---

func TestFormatDuration_ZeroDash(t *testing.T) {
	got := FormatDuration(0)
	if got == "" {
		t.Error("expected non-empty for zero duration")
	}
}

func TestFormatDuration_TwoAndHalfHours(t *testing.T) {
	got := FormatDuration(2*time.Hour + 30*time.Minute)
	if !strings.Contains(got, "2") {
		t.Errorf("expected hour representation, got %q", got)
	}
}

func TestFormatDuration_FortyFiveMinutes(t *testing.T) {
	got := FormatDuration(45 * time.Minute)
	if !strings.Contains(got, "45") {
		t.Errorf("expected 45 in output, got %q", got)
	}
}

// --- ListProjects nonexistent dir ---

func TestListProjects_NonexistentDir(t *testing.T) {
	projects, err := ListProjects("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if projects != nil {
		t.Error("expected nil for nonexistent dir")
	}
}

// --- ReadMetadata error ---

func TestReadMetadata_NonexistentFile(t *testing.T) {
	_, err := ReadMetadata("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent metadata")
	}
}

// --- ReadCheckpoint error ---

func TestReadCheckpoint_NonexistentFile(t *testing.T) {
	_, err := ReadCheckpoint("/nonexistent/path/cp.json")
	if err == nil {
		t.Error("expected error for nonexistent checkpoint")
	}
}

func TestReadCheckpoint_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0o644)
	_, err := ReadCheckpoint(path)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// --- countRetries with both failure types ---

func TestCountRetries_OnlyQAFails(t *testing.T) {
	es := boost3EventStore(t)
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s1", nil))
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s1", nil))

	rb := &ReportBuilder{es: es}
	count, err := rb.countRetries("s1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestCountRetries_OnlyReviewFails(t *testing.T) {
	es := boost3EventStore(t)
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "rev", "s1", nil))

	rb := &ReportBuilder{es: es}
	count, err := rb.countRetries("s1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

// --- isProcessAlive ---

func TestIsProcessAlive_Self(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestIsProcessAlive_NonexistentPID(t *testing.T) {
	if isProcessAlive(9999999) {
		t.Error("nonexistent PID should not be alive")
	}
}
