package runtime

import (
	"strings"
	"testing"
)

// TestCLIAdapter_Prepare_NeutralizesMetacharsInPaths pins the fix for the %q
// shell-expansion regression. %q produces POSIX double-quotes, under which
// $(...), backticks, and $VAR stay active in `sh -c`. QuoteShellArg uses single
// quotes, which neutralize them. A LogFile carrying a command-substitution
// payload must NOT appear unquoted (where the shell would execute it).
func TestCLIAdapter_Prepare_NeutralizesMetacharsInPaths(t *testing.T) {
	adapter := NewCLIAdapter("claude-code", "claude", nil, []string{"opus-4"})
	dir := t.TempDir()

	// Prepare does no I/O, so LogFile need not be a real path — inject a payload.
	payload := "/tmp/out$(touch /tmp/pwned).log"
	exec, err := adapter.Prepare(SessionConfig{
		SessionName: "s",
		WorkDir:     dir,
		Goal:        "do work",
		LogFile:     payload,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// The dangerous payload must be wrapped in single quotes (which the shell
	// treats literally), never left bare.
	if !strings.Contains(exec.Command, "'"+payload+"'") {
		t.Errorf("log-file payload not single-quoted (command-substitution risk):\n%s", exec.Command)
	}
	// Regression guard: %q would have emitted the payload inside double quotes.
	if strings.Contains(exec.Command, "\""+payload+"\"") {
		t.Errorf("command uses double-quotes (%%q) for log path — $(...) stays active under sh -c:\n%s", exec.Command)
	}
}

func TestCLIAdapter_Prepare_BasicCommand(t *testing.T) {
	adapter := NewCLIAdapter("claude-code", "claude", []string{"--dangerously-skip-permissions"}, []string{"opus-4"})
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		SessionName: "test-session",
		WorkDir:     dir,
		Model:       "claude-sonnet-4-5-20250514",
		Goal:        "implement login",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if exec.SessionName != "test-session" {
		t.Errorf("SessionName = %q, want test-session", exec.SessionName)
	}
	if exec.WorkDir != dir {
		t.Errorf("WorkDir = %q, want %q", exec.WorkDir, dir)
	}
	if !strings.Contains(exec.Command, "--model") {
		t.Error("command should contain --model")
	}
	if !strings.Contains(exec.Command, "claude-sonnet-4-5-20250514") {
		t.Error("command should contain model name")
	}
	if _, ok := exec.SetupFiles[dir+"/CLAUDE.md"]; !ok {
		t.Error("setup files should include CLAUDE.md")
	}
	// AGENTS.md is the Codex / generic-agent CLI counterpart. The
	// adapter dual-writes both files so non-Claude runtimes pick up
	// the same directive without a separate code path.
	if _, ok := exec.SetupFiles[dir+"/AGENTS.md"]; !ok {
		t.Error("setup files should include AGENTS.md (Codex / generic-agent directive)")
	}
}

func TestCLIAdapter_Prepare_RejectsUnsafeModel(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	_, err := adapter.Prepare(SessionConfig{
		Model:   "model; evil",
		WorkDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("should reject unsafe model name")
	}
}

func TestCLIAdapter_Prepare_RejectsUnsafeArg(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", []string{"--flag;evil"}, nil)
	_, err := adapter.Prepare(SessionConfig{
		WorkDir: t.TempDir(),
		Goal:    "test",
	})
	if err == nil {
		t.Fatal("should reject unsafe runtime arg")
	}
}

func TestCLIAdapter_Prepare_PromptFile(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", []string{"-p", "-"}, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir:      dir,
		Goal:         "do something",
		SystemPrompt: "you are an agent",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	promptPath := dir + "/.vxd-prompts/prompt.txt"
	content, ok := exec.SetupFiles[promptPath]
	if !ok {
		t.Fatal("prompt file not in setup files")
	}
	if !strings.Contains(content, "you are an agent") {
		t.Error("prompt file should contain system prompt")
	}
	if !strings.Contains(content, "do something") {
		t.Error("prompt file should contain goal")
	}
}

func TestCLIAdapter_Prepare_NoPromptWithoutGoal(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if strings.Contains(exec.Command, " -p ") {
		t.Error("command should not contain -p flag when no goal is set")
	}
}

func TestCLIAdapter_Prepare_LogFile(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir: dir,
		Goal:    "test",
		LogFile: "/tmp/test.log",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if !strings.Contains(exec.Command, "/tmp/test.log") {
		t.Error("command should reference log file when LogFile is set")
	}
	if exec.LogFile != "/tmp/test.log" {
		t.Errorf("LogFile = %q, want /tmp/test.log", exec.LogFile)
	}
}

func TestCLIAdapter_Prepare_EnvVars(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir: dir,
		Goal:    "test",
		EnvVars: map[string]string{"CUSTOM_VAR": "custom_value"},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if exec.Env["CUSTOM_VAR"] != "custom_value" {
		t.Error("env should contain custom var")
	}
	if !strings.Contains(exec.Command, "CUSTOM_VAR") {
		t.Error("command should export custom env var")
	}
}

func TestCLIAdapter_Prepare_UnsetsClaudeCode(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if !strings.Contains(exec.Command, "CLAUDECODE") {
		t.Error("command should unset CLAUDECODE")
	}
}

func TestCLIAdapter_Name(t *testing.T) {
	adapter := NewCLIAdapter("claude-code", "claude", nil, []string{"opus-4"})
	if adapter.Name() != "claude-code" {
		t.Errorf("Name() = %q, want claude-code", adapter.Name())
	}
	if len(adapter.SupportedModels()) != 1 || adapter.SupportedModels()[0] != "opus-4" {
		t.Errorf("SupportedModels() = %v, want [opus-4]", adapter.SupportedModels())
	}
}

func TestCLIAdapter_Prepare_CLAUDEMDContent(t *testing.T) {
	adapter := NewCLIAdapter("test", "claude", nil, nil)
	dir := t.TempDir()

	exec, err := adapter.Prepare(SessionConfig{
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	claudePath := dir + "/CLAUDE.md"
	content, ok := exec.SetupFiles[claudePath]
	if !ok {
		t.Fatal("CLAUDE.md not in setup files")
	}
	if !strings.Contains(content, "VXD Agent Directive") {
		t.Error("CLAUDE.md content should contain VXD Agent Directive")
	}
}
