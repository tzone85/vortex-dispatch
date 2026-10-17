package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func boost2EventStore(t *testing.T) *state.FileStore {
	t.Helper()
	dir := t.TempDir()
	fs, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

// --- extractJSON coverage tests ---

func TestExtractJSON_PlainArray(t *testing.T) {
	input := `[{"id":"s1"}]`
	got := extractJSON(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractJSON_PlainObject(t *testing.T) {
	input := `{"key":"value"}`
	got := extractJSON(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractJSON_CodeFenceJSON(t *testing.T) {
	input := "```json\n{\"key\":\"value\"}\n```"
	want := `{"key":"value"}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_CodeFencePlain(t *testing.T) {
	input := "```\n[1,2,3]\n```"
	want := "[1,2,3]"
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_PreambleBeforeArray(t *testing.T) {
	input := "Here is the result:\n[{\"id\":\"s1\"}]"
	want := `[{"id":"s1"}]`
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_PreambleBeforeObject(t *testing.T) {
	input := "Sure, here you go:\n{\"key\":\"value\"}"
	want := `{"key":"value"}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_EmbeddedCodeFenceInPreamble(t *testing.T) {
	input := "Here's the JSON:\n```json\n{\"ok\":true}\n```\nDone."
	want := `{"ok":true}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_EmbeddedPlainFenceInPreamble(t *testing.T) {
	input := "Result:\n```\n[1,2]\n```\nEnd."
	want := "[1,2]"
	got := extractJSON(input)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_EmptyString(t *testing.T) {
	got := extractJSON("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractJSON_WhitespaceOnly(t *testing.T) {
	got := extractJSON("   \n  ")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractJSON_PreambleNoJSON(t *testing.T) {
	input := "No JSON here at all"
	got := extractJSON(input)
	// Should return the input since no JSON delimiters found
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

// --- FlexibleString coverage tests ---

func TestFlexibleString_UnmarshalArray(t *testing.T) {
	data := `["line1","line2","line3"]`
	var f FlexibleString
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatal(err)
	}
	want := "line1\nline2\nline3"
	if string(f) != want {
		t.Errorf("expected %q, got %q", want, string(f))
	}
}

func TestFlexibleString_UnmarshalNumber(t *testing.T) {
	data := `42`
	var f FlexibleString
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatal(err)
	}
	if string(f) != "42" {
		t.Errorf("expected %q, got %q", "42", string(f))
	}
}

func TestFlexibleString_UnmarshalBool(t *testing.T) {
	data := `true`
	var f FlexibleString
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatal(err)
	}
	if string(f) != "true" {
		t.Errorf("expected %q, got %q", "true", string(f))
	}
}

// --- Checkpoint WriteCheckpoint error paths ---

func TestWriteCheckpoint_RenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "subdir", "checkpoint.json")
	cp := Checkpoint{
		ReqID:     "req-1",
		Phase:     PhaseDispatching,
		Timestamp: time.Now(),
		PID:       os.Getpid(),
	}
	err := WriteCheckpoint(path, cp)
	if err == nil {
		t.Error("expected error when parent dir doesn't exist")
	}
}

func TestCheckpoint_IsStale_Fresh(t *testing.T) {
	cp := Checkpoint{Timestamp: time.Now()}
	if cp.IsStale(1 * time.Hour) {
		t.Error("checkpoint created just now should not be stale")
	}
}

func TestCheckpoint_IsStale_Old(t *testing.T) {
	cp := Checkpoint{Timestamp: time.Now().Add(-2 * time.Hour)}
	if !cp.IsStale(1 * time.Hour) {
		t.Error("checkpoint 2h old should be stale with 1h threshold")
	}
}

// --- Project extractRepoName coverage ---

func TestExtractRepoName_SSHWithColon(t *testing.T) {
	got := extractRepoName("git@github.com:org/my-repo.git")
	if got != "my-repo" {
		t.Errorf("expected my-repo, got %q", got)
	}
}

func TestExtractRepoName_HTTPSNested(t *testing.T) {
	got := extractRepoName("https://github.com/org/sub/deep-repo.git")
	if got != "deep-repo" {
		t.Errorf("expected deep-repo, got %q", got)
	}
}

func TestExtractRepoName_SimpleName(t *testing.T) {
	got := extractRepoName("my-project")
	if got != "my-project" {
		t.Errorf("expected my-project, got %q", got)
	}
}

func TestExtractRepoName_EmptyInput(t *testing.T) {
	got := extractRepoName("")
	if got != "unnamed" {
		t.Errorf("expected unnamed, got %q", got)
	}
}

// --- LockFile coverage ---

func TestForceAcquireLock_OverridesExistingAndReads(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vxd.lock")

	// Write an existing lock
	info1, err := AcquireLock(lockPath, "req-old")
	if err != nil {
		t.Fatal(err)
	}
	if info1.ReqID != "req-old" {
		t.Errorf("expected req-old, got %q", info1.ReqID)
	}

	// Force acquire should override
	info2, err := ForceAcquireLock(lockPath, "req-new")
	if err != nil {
		t.Fatal(err)
	}
	if info2.ReqID != "req-new" {
		t.Errorf("expected req-new, got %q", info2.ReqID)
	}

	// Verify the lock file reflects the new lock
	read, err := ReadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if read.ReqID != "req-new" {
		t.Errorf("expected req-new after force, got %q", read.ReqID)
	}
}

func TestWriteLockFile_InvalidPath(t *testing.T) {
	// Parent component is a regular file → os.WriteFile fails with ENOTDIR for
	// any euid, and nothing leaks outside the temp dir. The old hard-coded
	// "/nonexistent/path/lock.json" target succeeded as root (its parent was
	// created by TestWriteMetadata_InvalidDir's polluting write), making the
	// assertion silently pass/fail depending on euid and test order.
	notDir := filepath.Join(t.TempDir(), "iamafile")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := writeLockFile(filepath.Join(notDir, "lock.json"), LockInfo{PID: 1})
	if err == nil {
		t.Error("expected error writing to invalid path")
	}
}

// --- ReviewGate StoryApproved coverage ---

func TestReviewGate_StoryApproved_NoEvents(t *testing.T) {
	es := boost2EventStore(t)
	gate := NewReviewGate(es)

	got := gate.StoryApproved("story-1")
	if got {
		t.Error("expected false when no approval events exist")
	}
}

func TestReviewGate_StoryApproved_WithEvent(t *testing.T) {
	es := boost2EventStore(t)
	evt := state.NewEvent(state.EventStoryApproved, "user", "story-1", map[string]any{})
	es.Append(evt)

	gate := NewReviewGate(es)
	got := gate.StoryApproved("story-1")
	if !got {
		t.Error("expected true when approval event exists")
	}
}

func TestReviewGate_StoryApproved_DifferentStory(t *testing.T) {
	es := boost2EventStore(t)
	evt := state.NewEvent(state.EventStoryApproved, "user", "story-other", map[string]any{})
	es.Append(evt)

	gate := NewReviewGate(es)
	got := gate.StoryApproved("story-1")
	if got {
		t.Error("expected false for different story ID")
	}
}

// --- Reputation BestAgentForRole edge cases ---

func TestBestAgentForRole_EmptyMap(t *testing.T) {
	reps := map[string]agent.AgentReputation{}
	got := BestAgentForRole(reps, "junior")
	if got != "" {
		t.Errorf("expected empty for empty map, got %q", got)
	}
}

func TestBestAgentForRole_ShortAgentID(t *testing.T) {
	reps := map[string]agent.AgentReputation{
		"ab": {AgentID: "ab"},
	}
	got := BestAgentForRole(reps, "junior")
	if got != "" {
		t.Errorf("expected empty for too-short agent ID, got %q", got)
	}
}

// --- Escalation RetryCountAtCurrentTier error paths ---

func TestRetryCountAtCurrentTier_NoEscalations(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	count, err := em.RetryCountAtCurrentTier("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestRetryCountAtCurrentTier_WithFailures(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "story-1", nil))
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "story-1", nil))

	count, err := em.RetryCountAtCurrentTier("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

// --- ShouldEscalate tests ---

func TestShouldEscalate_NoFailures(t *testing.T) {
	es := boost2EventStore(t)
	cfg := config.RoutingConfig{MaxRetriesBeforeEscalation: 2, MaxSeniorRetries: 2, MaxManagerAttempts: 2}
	em := NewEscalationMachine(es, cfg)

	shouldEsc, tier, err := em.ShouldEscalate("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if shouldEsc {
		t.Error("should not escalate with no failures")
	}
	if tier != 0 {
		t.Errorf("expected tier 0, got %d", tier)
	}
}

func TestShouldEscalate_AtLimit(t *testing.T) {
	es := boost2EventStore(t)
	cfg := config.RoutingConfig{MaxRetriesBeforeEscalation: 1}
	em := NewEscalationMachine(es, cfg)

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "story-1", nil))

	shouldEsc, tier, err := em.ShouldEscalate("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if !shouldEsc {
		t.Error("should escalate when at retry limit")
	}
	if tier != 1 {
		t.Errorf("expected tier 1, got %d", tier)
	}
}

// --- Report countRetries coverage ---

func TestReportBuilder_CountRetries_MixedEvents(t *testing.T) {
	es := boost2EventStore(t)
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s1", nil))
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s1", nil))
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s1", nil))

	rb := &ReportBuilder{es: es}
	count, err := rb.countRetries("s1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 retries, got %d", count)
	}
}

func TestReportBuilder_CountRetries_NoEvents(t *testing.T) {
	es := boost2EventStore(t)
	rb := &ReportBuilder{es: es}
	count, err := rb.countRetries("s1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 retries, got %d", count)
	}
}

// --- Report storyDuration edge cases ---

func TestStoryDuration_MissingMergedAt(t *testing.T) {
	rb := &ReportBuilder{}
	s := state.Story{CreatedAt: time.Now()}
	d := rb.storyDuration(s)
	if d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}

func TestStoryDuration_MissingCreatedAt(t *testing.T) {
	rb := &ReportBuilder{}
	s := state.Story{MergedAt: time.Now()}
	d := rb.storyDuration(s)
	if d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}

func TestStoryDuration_ValidDuration(t *testing.T) {
	rb := &ReportBuilder{}
	now := time.Now()
	s := state.Story{
		CreatedAt: now.Add(-1 * time.Hour),
		MergedAt:  now,
	}
	d := rb.storyDuration(s)
	if d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expected ~1h, got %v", d)
	}
}

// --- RenderMarkdown coverage ---

func TestRenderMarkdown_EmptyTimeline(t *testing.T) {
	data := ReportData{
		Title:         "Test Req",
		RequirementID: "r1",
		Description:   "Desc",
		GeneratedAt:   time.Now(),
		Stories:       []ReportStory{},
		Effort:        Estimate{Summary: EstimateSummary{Currency: "USD"}},
		Timeline:      []TimelineEntry{},
	}
	md := RenderMarkdown(data, "myproject", false)
	if !strings.Contains(md, "No significant events recorded") {
		t.Error("expected empty timeline message")
	}
}

func TestRenderMarkdown_InternalWithAttempts(t *testing.T) {
	data := ReportData{
		Title:         "Test Req",
		RequirementID: "r1",
		Description:   "Desc",
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{
				Title:      "Story 1",
				Status:     "merged",
				Complexity: 3,
				Attempts: []Attempt{
					{Number: 1, Role: "junior", Outcome: "failed", Error: "build error", Duration: 5 * time.Minute},
					{Number: 2, Role: "senior", Outcome: "success", Duration: 10 * time.Minute},
				},
			},
		},
		Effort:   Estimate{Summary: EstimateSummary{Currency: "USD"}},
		Timeline: []TimelineEntry{{Timestamp: time.Now(), EventType: "test", Description: "test event"}},
	}
	md := RenderMarkdown(data, "proj", true)
	if !strings.Contains(md, "Attempt History") {
		t.Error("expected attempt history section for internal report")
	}
	if !strings.Contains(md, "build error") {
		t.Error("expected error detail in attempt history")
	}
}

func TestRenderMarkdown_WithPRNumber(t *testing.T) {
	data := ReportData{
		Title:         "PR test",
		RequirementID: "r1",
		Description:   "Desc",
		GeneratedAt:   time.Now(),
		Stories: []ReportStory{
			{Title: "S1", Status: "merged", Complexity: 2, PRNumber: 42, PRUrl: "https://github.com/org/repo/pull/42"},
		},
		Effort:   Estimate{Summary: EstimateSummary{Currency: "USD"}},
		Timeline: []TimelineEntry{},
	}
	md := RenderMarkdown(data, "proj", false)
	if !strings.Contains(md, "#42") {
		t.Error("expected PR number in deliverables table")
	}
}

// --- execExpandHome ---

func TestExecExpandHome_AbsolutePath(t *testing.T) {
	got := execExpandHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %q", got)
	}
}

func TestExecExpandHome_TildeExpansion(t *testing.T) {
	got := execExpandHome("~/some/path")
	if got == "~/some/path" {
		t.Error("expected tilde to be expanded")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestExecExpandHome_EmptyInput(t *testing.T) {
	got := execExpandHome("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// --- LastEscalationTime coverage ---

func TestLastEscalationTime_NoEvents(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	ts, err := em.lastEscalationTime("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time, got %v", ts)
	}
}

func TestLastEscalationTime_MultipleEvents(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	// Add two escalation events
	evt1 := state.NewEvent(state.EventStoryEscalated, "monitor", "story-1", map[string]any{
		"from_tier": 0, "to_tier": float64(1),
	})
	es.Append(evt1)

	time.Sleep(10 * time.Millisecond) // ensure different timestamp

	evt2 := state.NewEvent(state.EventStoryEscalated, "monitor", "story-1", map[string]any{
		"from_tier": 1, "to_tier": float64(2),
	})
	es.Append(evt2)

	ts, err := em.lastEscalationTime("story-1")
	if err != nil {
		t.Fatal(err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero time")
	}
}

// --- MaxRetriesForTier coverage ---

func TestMaxRetriesForTier_AllTiers(t *testing.T) {
	cfg := config.RoutingConfig{
		MaxRetriesBeforeEscalation: 3,
		MaxSeniorRetries:           2,
		MaxManagerAttempts:         1,
	}
	em := NewEscalationMachine(boost2EventStore(t), cfg)

	tests := []struct {
		tier int
		want int
	}{
		{0, 3},
		{1, 2},
		{2, 1},
		{3, 1},
		{4, 0},
		{99, 0},
	}
	for _, tc := range tests {
		got := em.MaxRetriesForTier(tc.tier)
		if got != tc.want {
			t.Errorf("tier %d: expected %d, got %d", tc.tier, tc.want, got)
		}
	}
}

// --- MigrateOldLayout edge cases ---

func TestMigrateOldLayout_AlreadyMigrated(t *testing.T) {
	dir := t.TempDir()
	// Create projects/ directory so migration is skipped
	os.MkdirAll(filepath.Join(dir, "projects"), 0o755)

	migrated, err := MigrateOldLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Error("expected migration to be skipped when projects/ exists")
	}
}

func TestMigrateOldLayout_NoOldSentinelFile(t *testing.T) {
	dir := t.TempDir()
	// No events.jsonl sentinel file

	migrated, err := MigrateOldLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Error("expected migration to be skipped when no old files exist")
	}
}

// --- WriteMetadata error path ---

func TestWriteMetadata_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "new-project")

	meta := ProjectMetadata{
		Name:      "test-proj",
		RepoPath:  "/some/path",
		CreatedAt: time.Now(),
	}
	err := WriteMetadata(projectDir, meta)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	read, err := ReadMetadata(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if read.Name != "test-proj" {
		t.Errorf("expected test-proj, got %q", read.Name)
	}
}

// --- ListProjects with mixed entries ---

func TestListProjects_SkipsFiles(t *testing.T) {
	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	os.MkdirAll(projectsDir, 0o755)

	// Create a valid project
	validDir := filepath.Join(projectsDir, "valid")
	os.MkdirAll(validDir, 0o755)
	WriteMetadata(validDir, ProjectMetadata{Name: "valid"})

	// Create a directory without metadata (should be skipped)
	os.MkdirAll(filepath.Join(projectsDir, "invalid"), 0o755)

	// Create a file in projects dir (should be skipped)
	os.WriteFile(filepath.Join(projectsDir, "somefile.txt"), []byte("hi"), 0o644)

	projects, err := ListProjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}
}

// --- ComputeReputationFromEvents ---

func TestComputeReputationFromEvents_Empty(t *testing.T) {
	rep := ComputeReputationFromEvents(nil)
	if rep.TotalStories != 0 {
		t.Errorf("expected 0 stories, got %d", rep.TotalStories)
	}
}

func TestComputeReputationFromEvents_WithEscalation(t *testing.T) {
	events := []state.Event{
		{
			AgentID: "agent-1",
			StoryID: "s1",
			Payload: mustMarshal(map[string]any{"quality_score": 4.0, "duration_s": 120.0, "was_escalated": true}),
		},
	}
	rep := ComputeReputationFromEvents(events)
	if rep.TotalStories != 1 {
		t.Errorf("expected 1, got %d", rep.TotalStories)
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// --- AgentReputations ---

func TestAgentReputations_Empty(t *testing.T) {
	es := boost2EventStore(t)
	reps, err := AgentReputations(es)
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 0 {
		t.Errorf("expected empty map, got %d", len(reps))
	}
}

func TestAgentReputations_GroupsByAgent(t *testing.T) {
	es := boost2EventStore(t)
	es.Append(state.NewEvent(state.EventStoryQAPassed, "agent-a", "s1",
		map[string]any{"quality_score": 5.0, "duration_s": 60.0}))
	es.Append(state.NewEvent(state.EventStoryQAFailed, "agent-b", "s2",
		map[string]any{"quality_score": 2.0, "duration_s": 300.0}))

	reps, err := AgentReputations(es)
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 2 {
		t.Errorf("expected 2 agents, got %d", len(reps))
	}
}

// --- ValidateSplit edge cases ---

func TestValidateSplit_MaxDepthExceeded(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	err := em.ValidateSplit(2, []SplitChild{{ID: "c1", Complexity: 1}}, 5)
	if err == nil {
		t.Error("expected error when max split depth exceeded")
	}
}

func TestValidateSplit_DuplicateOwnedFiles(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	children := []SplitChild{
		{ID: "c1", OwnedFiles: []string{"main.go"}, Complexity: 1},
		{ID: "c2", OwnedFiles: []string{"main.go"}, Complexity: 1},
	}
	err := em.ValidateSplit(0, children, 5)
	if err == nil {
		t.Error("expected error for overlapping owned files")
	}
}

func TestValidateSplit_ExcessiveComplexity(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	children := []SplitChild{
		{ID: "c1", OwnedFiles: []string{"a.go"}, Complexity: 10},
	}
	err := em.ValidateSplit(0, children, 5)
	if err == nil {
		t.Error("expected error for excessive complexity")
	}
}

func TestValidateSplit_ValidNoOverlap(t *testing.T) {
	es := boost2EventStore(t)
	cfg := defaultRoutingConfig()
	em := NewEscalationMachine(es, cfg)

	children := []SplitChild{
		{ID: "c1", Suffix: "c1", OwnedFiles: []string{"a.go"}, Complexity: 3},
		{ID: "c2", Suffix: "c2", OwnedFiles: []string{"b.go"}, Complexity: 2},
	}
	err := em.ValidateSplit(0, children, 5)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Suppress unused import warnings
var _ = agent.AgentReputation{}
