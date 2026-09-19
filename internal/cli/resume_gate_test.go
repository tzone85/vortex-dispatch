package cli

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRunResume_DryRun_UnsettledStaysUnsettled: every story is complete and
// the requirement has no verdict, but --dry-run wires no completion gate —
// dispatchNextWave would fall through to the advisory verification, which
// emits REQ_COMPLETED whatever it found. A flag that promises to simulate
// must not write a terminal verdict, so resume says the gate is not active,
// changes nothing and exits with ErrNoVerdict. The gate itself is reached
// from a zero-agent monitor in
// TestMonitor_NoAgents_RunsCompletionGate_RedThenGreen, where one is wired.
func TestRunResume_DryRun_UnsettledStaysUnsettled(t *testing.T) {
	runResumeGateOnly(t, nil, ErrNoVerdict)
}

// TestRunResume_DryRun_BlockedStaysBlocked: the same for the case the
// advisory path would have overturned — a red REQ_BLOCKED becoming
// REQ_COMPLETED. Nothing is unblocked either: no REQ_RESUMED, and the exit
// is ErrRequirementBlocked.
func TestRunResume_DryRun_BlockedStaysBlocked(t *testing.T) {
	runResumeGateOnly(t, []state.Event{state.NewEvent(state.EventReqBlocked, "monitor", "", map[string]any{"id": "REQ-GATE1"})}, ErrRequirementBlocked)
}

// runResumeGateOnly seeds a requirement with one merged story (plus extra
// events, e.g. a REQ_BLOCKED verdict), runs resume in dry-run and asserts
// that it reported the inactive gate, left the status exactly as it found it
// and exited with wantErr.
func runResumeGateOnly(t *testing.T, extra []state.Event, wantErr error) {
	t.Helper()
	dir, s := setupTestEnv(t)

	events := []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{"id": "REQ-GATE1", "title": "Re-run the gate"}),
		state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{"req_id": "REQ-GATE1"}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-GATE1", map[string]any{
			"id": "STR-GATE1", "req_id": "REQ-GATE1", "title": "Merged story", "complexity": 3,
		}),
		state.NewEvent(state.EventStoryMerged, "", "STR-GATE1", nil),
	}
	events = append(events, extra...)
	for _, evt := range events {
		if err := s.Events.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
		if err := s.Proj.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}
	req, err := s.Proj.GetRequirement("REQ-GATE1")
	if err != nil || requirementSettled(req.Status) {
		t.Fatalf("precondition: an unsettled requirement (err=%v, status=%q)", err, req.Status)
	}
	wantStatus := req.Status // whatever it is, the dry run must not change it
	s.Close()

	// A short poll interval so the monitor's first tick is immediate; the
	// rest of the config is the default.
	cfgPath := filepath.Join(dir, "vxd.yaml")
	if err := os.WriteFile(cfgPath, []byte("monitor:\n  poll_interval_ms: 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// resume runs in a repo that is NOT the state dir's parent: the completion
	// path stashes the working tree (git stash -u) before pulling the base
	// branch, which would swap an in-repo .vxd/ from under the open stores.
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@test.com"}, {"config", "user.name", "Test"}, {"config", "core.autocrlf", "false"}, {"config", "core.eol", "lf"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", cfgPath, "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-GATE1", "--dry-run"})

	var buf, logs bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// The monitor and the verification loop report through the log; keep it
	// so a failure says where the path stopped.
	prevLog := log.Writer()
	log.SetOutput(io.MultiWriter(prevLog, &logs))
	t.Cleanup(func() { log.SetOutput(prevLog) })

	err = cmd.Execute()
	out := buf.String()
	if !errors.Is(err, wantErr) {
		t.Fatalf("resume must exit with %v, got %v\n%s", wantErr, err, out)
	}
	if !strings.Contains(out, "the completion gate is not active") {
		t.Errorf("resume must say why nothing re-ran the gate:\n%s", out)
	}
	for _, reject := range []string{"re-running the completion gate", "No agents to track", "Unblocked"} {
		if strings.Contains(out, reject) {
			t.Errorf("a dry run must not pretend to run the gate (%q):\n%s", reject, out)
		}
	}

	ps, err := state.NewSQLiteStore(filepath.Join(dir, ".vxd", "projects", "test-project", "vxd.db"))
	if err != nil {
		t.Fatalf("reopen projection: %v", err)
	}
	defer ps.Close()
	req, err = ps.GetRequirement("REQ-GATE1")
	if err != nil {
		t.Fatalf("requirement after resume: %v", err)
	}
	evts, _ := os.ReadFile(filepath.Join(dir, ".vxd", "projects", "test-project", "events.jsonl"))
	if req.Status != wantStatus {
		t.Fatalf("status %q, want %q (unchanged)\n--- output:\n%s\n--- log:\n%s\n--- events.jsonl:\n%s", req.Status, wantStatus, out, logs.String(), evts)
	}
	for _, forbidden := range []string{`"REQ_COMPLETED"`, `"REQ_RESUMED"`} {
		if strings.Contains(string(evts), forbidden) {
			t.Fatalf("a dry run must append no %s\n--- events.jsonl:\n%s", forbidden, evts)
		}
	}
}

// TestResumeOutcome: the exit-code contract of `vxd resume`, as a table —
// blocked and "no verdict" are errors so `vxd resume X && ./deploy.sh` cannot
// deploy a mainline the gate did not pass, and a detach from running agents
// is not a failure.
func TestResumeOutcome(t *testing.T) {
	cases := []struct {
		name                     string
		status                   string
		allComplete, interrupted bool
		want                     error
		wantOut                  string
	}{
		{name: "completed", status: "completed", allComplete: true, wantOut: "SUMMARY"},
		{name: "blocked", status: "blocked", allComplete: true, want: ErrRequirementBlocked},
		{name: "blocked and interrupted", status: "blocked", allComplete: true, interrupted: true, want: ErrRequirementBlocked},
		{name: "gate reached no verdict", status: "planned", allComplete: true, want: ErrNoVerdict},
		{name: "signal during the gate", status: "in_progress", allComplete: true, interrupted: true, want: ErrNoVerdict},
		{name: "detached from running agents", status: "in_progress", interrupted: true, wantOut: "Detached from"},
		{name: "stalled: stories incomplete", status: "in_progress", want: ErrIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := resumeOutcome(&buf, resumeResult{
				reqID: "REQ-1", repoDir: "/repo", status: tc.status,
				allComplete: tc.allComplete, interrupted: tc.interrupted,
				summary: func() (string, error) { return "SUMMARY", nil },
			})
			if tc.want == nil && err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if tc.wantOut != "" && !strings.Contains(buf.String(), tc.wantOut) {
				t.Fatalf("output lacks %q: %q", tc.wantOut, buf.String())
			}
			if tc.want != nil && buf.Len() != 0 {
				t.Fatalf("an error path prints nothing (the message is the error), got %q", buf.String())
			}
		})
	}
}

// TestResumeOutcome_BlockedNamesTheGapsFile: the error is the only thing the
// operator sees, so it has to carry the next step.
func TestResumeOutcome_BlockedNamesTheGapsFile(t *testing.T) {
	err := resumeOutcome(io.Discard, resumeResult{reqID: "REQ-1", repoDir: "/repo", status: "blocked", allComplete: true})
	for _, want := range []string{".vxd-fix-gaps.md", "/repo", "vxd resume REQ-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the blocked error must name %q: %v", want, err)
		}
	}
}

// TestPrepareGateOnly_GateDisabled_UnsettledGoesThrough: with
// qa.disable_completion_gate the advisory path is what settles a requirement
// — "the requirement always completes", as the config documents. Refusing
// here left an all-merged requirement no command could finish: every resume
// answered "run: vxd resume X", which is the command that just refused.
func TestPrepareGateOnly_GateDisabled_UnsettledGoesThrough(t *testing.T) {
	dir, s := setupTestEnv(t)
	defer s.Close()
	for _, evt := range []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{"id": "REQ-GD1", "title": "Gate disabled"}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-GD1", map[string]any{
			"id": "STR-GD1", "req_id": "REQ-GD1", "title": "Merged story", "complexity": 3,
		}),
		state.NewEvent(state.EventStoryMerged, "", "STR-GD1", nil),
	} {
		if err := s.Events.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
		if err := s.Proj.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}

	var buf bytes.Buffer
	gateOnly, done, err := prepareGateOnly(&buf, s, "REQ-GD1", true, false, false, dir)
	if err != nil || done {
		t.Fatalf("a disabled gate must not stop resume: gateOnly=%v done=%v err=%v\n%s", gateOnly, done, err, buf.String())
	}
	if !gateOnly {
		t.Fatalf("the run still exists to settle the requirement: %s", buf.String())
	}
}

// TestPrepareGateOnly_GateDisabled_BlockedRefuses: the advisory path completes
// whatever it finds, so with a red verdict on record and no real gate there is
// nothing that can honestly re-verify it.
func TestPrepareGateOnly_GateDisabled_BlockedRefuses(t *testing.T) {
	dir, s := setupTestEnv(t)
	defer s.Close()
	for _, evt := range []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{"id": "REQ-GD2", "title": "Blocked, gate off"}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-GD2", map[string]any{
			"id": "STR-GD2", "req_id": "REQ-GD2", "title": "Merged story", "complexity": 3,
		}),
		state.NewEvent(state.EventStoryMerged, "", "STR-GD2", nil),
		state.NewEvent(state.EventReqBlocked, "monitor", "", map[string]any{"id": "REQ-GD2"}),
	} {
		if err := s.Events.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
		if err := s.Proj.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}

	var buf bytes.Buffer
	gateOnly, done, err := prepareGateOnly(&buf, s, "REQ-GD2", true, false, false, dir)
	if gateOnly || !done || !errors.Is(err, ErrRequirementBlocked) {
		t.Fatalf("gateOnly=%v done=%v err=%v, want a refusal\n%s", gateOnly, done, err, buf.String())
	}
}

// TestWarnDirtyTree_IgnoresVXDArtefacts: a blocked requirement always leaves
// .vxd-fix-gaps.md behind, and the pull rewrites .gitignore, so a note that
// counted them would fire on the very flow it exists for — and be ignored.
func TestWarnDirtyTree_IgnoresVXDArtefacts(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"},
		{"config", "core.autocrlf", "false"}, {"config", "core.eol", "lf"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if outB, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, outB)
		}
	}
	// .gitignore is committed first: the pull appends to it, so the note has
	// to ignore it as a MODIFIED tracked file, not only as an untracked one.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".gitignore"}, {"commit", "-q", "-m", "ignore node_modules"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if outB, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, outB)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n.vxd-fix-gaps.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".vxd-fix-gaps.md", "WAVE_CONTEXT.md"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	warnDirtyTree(&buf, repo)
	if buf.Len() != 0 {
		t.Fatalf("VXD's own artefacts are not the operator's uncommitted work: %s", buf.String())
	}

	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	warnDirtyTree(&buf, repo)
	if !strings.Contains(buf.String(), "uncommitted changes") {
		t.Fatalf("a real edit must be reported: %q", buf.String())
	}
}
