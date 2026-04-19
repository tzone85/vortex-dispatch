package engine

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestTierForRole(t *testing.T) {
	tests := []struct {
		role agent.Role
		want int
	}{
		{agent.RoleJunior, 0},
		{agent.RoleIntermediate, 0},
		{agent.RoleSenior, 1},
		{agent.RoleManager, 2},
		{agent.RoleTechLead, 3},
		{agent.RoleQA, 0},
		{agent.RoleSupervisor, 0},
		{agent.Role("unknown"), 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := tierForRole(tt.role)
			if got != tt.want {
				t.Errorf("tierForRole(%s) = %d, want %d", tt.role, got, tt.want)
			}
		})
	}
}

func TestRuntimeForRole_AnthropicProvider(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Senior: config.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-4-6-20250620"},
			},
			Runtimes: map[string]config.RuntimeConfig{
				"claude-code": {Command: "claude"},
			},
		},
	}
	got := e.runtimeForRole(agent.RoleSenior)
	if got != "claude-code" {
		t.Errorf("expected claude-code, got %s", got)
	}
}

func TestRuntimeForRole_GoogleProvider(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Junior: config.ModelConfig{Provider: "google", Model: "gemma-4"},
			},
			Runtimes: map[string]config.RuntimeConfig{
				"gemini": {Command: "gemini"},
			},
		},
	}
	got := e.runtimeForRole(agent.RoleJunior)
	if got != "gemini" {
		t.Errorf("expected gemini, got %s", got)
	}
}

func TestRuntimeForRole_OpenAIProvider(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Intermediate: config.ModelConfig{Provider: "openai", Model: "gpt-4"},
			},
			Runtimes: map[string]config.RuntimeConfig{
				"codex": {Command: "codex"},
			},
		},
	}
	got := e.runtimeForRole(agent.RoleIntermediate)
	if got != "codex" {
		t.Errorf("expected codex, got %s", got)
	}
}

func TestRuntimeForRole_GeminiAlias(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Junior: config.ModelConfig{Provider: "gemini", Model: "gemma-4"},
			},
			Runtimes: map[string]config.RuntimeConfig{
				"gemini": {Command: "gemini"},
			},
		},
	}
	got := e.runtimeForRole(agent.RoleJunior)
	if got != "gemini" {
		t.Errorf("expected gemini, got %s", got)
	}
}

func TestRuntimeForRole_Fallback(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Junior: config.ModelConfig{Provider: "custom-provider", Model: "custom-model"},
			},
			Runtimes: map[string]config.RuntimeConfig{
				"custom-runtime": {Command: "custom"},
			},
		},
	}
	got := e.runtimeForRole(agent.RoleJunior)
	if got != "custom-runtime" {
		t.Errorf("expected custom-runtime (fallback), got %s", got)
	}
}

func TestRuntimeForRole_NoRuntimes(t *testing.T) {
	e := &Executor{
		config: config.Config{
			Models: config.ModelsConfig{
				Junior: config.ModelConfig{Provider: "custom-provider", Model: "m"},
			},
			Runtimes: map[string]config.RuntimeConfig{},
		},
	}
	got := e.runtimeForRole(agent.RoleJunior)
	if got != "claude-code" {
		t.Errorf("expected claude-code (default fallback), got %s", got)
	}
}

func TestExecExpandHome_NoTilde(t *testing.T) {
	got := execExpandHome("/usr/local/bin")
	if got != "/usr/local/bin" {
		t.Errorf("expected /usr/local/bin, got %s", got)
	}
}

func TestExecExpandHome_WithTilde(t *testing.T) {
	got := execExpandHome("~/bin")
	if got == "~/bin" {
		t.Error("expected tilde to be expanded")
	}
}

func TestExecExpandHome_EmptyString(t *testing.T) {
	got := execExpandHome("")
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestLatestReviewFeedback_NoEvents(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "" {
		t.Errorf("expected empty string with no events, got %q", got)
	}
}

func TestLatestReviewFeedback_WithSummary(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "Missing error handling in handler.go",
	}))

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "Missing error handling in handler.go" {
		t.Errorf("expected feedback summary, got %q", got)
	}
}

func TestLatestReviewFeedback_WithReason(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "monitor", "s-001", map[string]any{
		"reason": "Tests failing after refactor",
	}))

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "Tests failing after refactor" {
		t.Errorf("expected feedback reason, got %q", got)
	}
}

func TestLatestReviewFeedback_SummaryTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "from-summary",
		"reason":  "from-reason",
	}))

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "from-summary" {
		t.Errorf("expected summary to take precedence, got %q", got)
	}
}

func TestLatestReviewFeedback_PicksLatestEvent(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "first-feedback",
	}))
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "second-feedback",
	}))

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "second-feedback" {
		t.Errorf("expected latest feedback, got %q", got)
	}
}

func TestLatestReviewFeedback_NilPayload(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	// Create an event with nil payload by manually constructing it
	evt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", nil)
	// Force nil payload
	evt.Payload = nil
	es.Append(evt)

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "" {
		t.Errorf("expected empty string for nil payload, got %q", got)
	}
}

func TestLatestReviewFeedback_InvalidPayload(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	evt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", nil)
	evt.Payload = json.RawMessage(`not valid json`)
	es.Append(evt)

	e := &Executor{eventStore: es}
	got := e.latestReviewFeedback("s-001")
	if got != "" {
		t.Errorf("expected empty string for invalid JSON payload, got %q", got)
	}
}
