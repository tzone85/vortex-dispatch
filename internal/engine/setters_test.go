package engine

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestMonitor_SetArtifactStore(t *testing.T) {
	m := &Monitor{}
	m.SetArtifactStore(nil) // nil is valid (disables artifact persistence)
	if m.artifactStore != nil {
		t.Error("expected nil artifact store")
	}
}

func TestMonitor_SetConflictResolver(t *testing.T) {
	m := &Monitor{}
	cr := &ConflictResolver{}
	m.SetConflictResolver(cr)
	if m.conflictResolver != cr {
		t.Error("expected conflict resolver to be set")
	}
}

func TestMonitor_SetReviewGate(t *testing.T) {
	m := &Monitor{}
	rg := &ReviewGate{}
	m.SetReviewGate(rg)
	if m.reviewGate != rg {
		t.Error("expected review gate to be set")
	}
}

func TestMonitor_SetCheckpointPath(t *testing.T) {
	m := &Monitor{}
	m.SetCheckpointPath("/tmp/checkpoint.json")
	if m.checkpointPath != "/tmp/checkpoint.json" {
		t.Errorf("expected checkpoint path, got %q", m.checkpointPath)
	}
}

func TestMonitor_SetManager(t *testing.T) {
	m := &Monitor{}
	mgr := &Manager{}
	m.SetManager(mgr)
	if m.manager != mgr {
		t.Error("expected manager to be set")
	}
}

func TestMonitor_SetPlanner(t *testing.T) {
	m := &Monitor{}
	p := &Planner{}
	m.SetPlanner(p)
	if m.planner != p {
		t.Error("expected planner to be set")
	}
}

func TestExecutor_SetArtifactStore(t *testing.T) {
	e := &Executor{}
	e.SetArtifactStore(nil)
	if e.artifactStore != nil {
		t.Error("expected nil artifact store")
	}
}

func TestExecutor_SetScratchboard(t *testing.T) {
	e := &Executor{}
	e.SetScratchboard(nil)
	if e.scratchboard != nil {
		t.Error("expected nil scratchboard")
	}
}

func TestExecutor_SetProjectDir(t *testing.T) {
	e := &Executor{}
	e.SetProjectDir("/home/user/.vxd/projects/test")
	if e.projectDir != "/home/user/.vxd/projects/test" {
		t.Errorf("expected project dir, got %q", e.projectDir)
	}
}

func TestNewConflictResolver(t *testing.T) {
	cr := NewConflictResolver(nil, "test-model", nil, "", 4096, nil, nil)
	if cr == nil {
		t.Fatal("expected non-nil conflict resolver")
	}
	if cr.model != "test-model" {
		t.Errorf("expected model test-model, got %q", cr.model)
	}
	if cr.maxTokens != 4096 {
		t.Errorf("expected maxTokens 4096, got %d", cr.maxTokens)
	}
	if cr.maxRounds != 10 {
		t.Errorf("expected default maxRounds 10, got %d", cr.maxRounds)
	}
}

func TestNewMonitor(t *testing.T) {
	cfg := config.Config{
		Routing: config.RoutingConfig{
			MaxRetriesBeforeEscalation: 2,
		},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, nil, nil)
	if m == nil {
		t.Fatal("expected non-nil monitor")
	}
	if m.escalation == nil {
		t.Error("expected escalation machine to be initialized")
	}
}

func TestNewMerger(t *testing.T) {
	cfg := config.MergeConfig{
		AutoMerge:  true,
		BaseBranch: "main",
	}
	m := NewMerger(cfg, nil, nil, nil)
	if m == nil {
		t.Fatal("expected non-nil merger")
	}
	if !m.config.AutoMerge {
		t.Error("expected auto merge true")
	}
}

func TestNewDispatcher(t *testing.T) {
	cfg := config.Config{
		Routing: config.RoutingConfig{JuniorMaxComplexity: 3},
	}
	d := NewDispatcher(cfg, nil, nil)
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}

func TestNewExecutor(t *testing.T) {
	cfg := config.Config{}
	e := NewExecutor(nil, cfg, nil, nil)
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestNewQA(t *testing.T) {
	cfg := QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}
	qa := NewQA(cfg, nil, nil, nil)
	if qa == nil {
		t.Fatal("expected non-nil QA")
	}
	if qa.config.LintCommand != "golangci-lint run" {
		t.Errorf("expected lint command, got %q", qa.config.LintCommand)
	}
}
