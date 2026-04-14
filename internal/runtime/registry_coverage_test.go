package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// TestBuildCommand_SystemPromptAndGoal verifies combined system prompt + goal.
func TestBuildCommand_SystemPromptAndGoal(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	scfg := SessionConfig{
		WorkDir:      dir,
		SystemPrompt: "You are a helpful assistant.",
		Goal:         "Implement login feature",
	}
	cmd, err := rt.BuildCommand(scfg)
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !strings.Contains(cmd, " -p ") {
		t.Error("expected -p flag for prompt")
	}

	// Verify the prompt file was written with system prompt + goal
	promptFile := filepath.Join(dir, ".vxd-prompts", "prompt.txt")
	data, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("reading prompt file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "You are a helpful assistant.") {
		t.Error("prompt file should contain system prompt")
	}
	if !strings.Contains(content, "Implement login feature") {
		t.Error("prompt file should contain goal")
	}
	if !strings.Contains(content, "---") {
		t.Error("prompt file should contain separator between system prompt and goal")
	}
}

// TestBuildCommand_LogFile verifies log file tee is added.
func TestBuildCommand_LogFile(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	logFile := filepath.Join(dir, "output.log")
	scfg := SessionConfig{
		WorkDir: dir,
		Goal:    "do task",
		LogFile: logFile,
	}
	cmd, err := rt.BuildCommand(scfg)
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !strings.Contains(cmd, "2>&1 | tee") {
		t.Error("expected tee for log file")
	}
	if !strings.Contains(cmd, "output.log") {
		t.Error("expected log file path in command")
	}
}

// TestBuildCommand_EnvVars verifies custom env vars are exported.
func TestBuildCommand_EnvVars(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	scfg := SessionConfig{
		WorkDir: dir,
		Goal:    "task",
		EnvVars: map[string]string{
			"CUSTOM_VAR": "custom_value",
		},
	}
	cmd, err := rt.BuildCommand(scfg)
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !strings.Contains(cmd, "CUSTOM_VAR") {
		t.Error("expected CUSTOM_VAR in command")
	}
	if !strings.Contains(cmd, "custom_value") {
		t.Error("expected custom_value in command")
	}
}

// TestBuildCommand_UnsetClaudeCode verifies CLAUDECODE is unset.
func TestBuildCommand_UnsetClaudeCode(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	cmd, err := rt.BuildCommand(SessionConfig{WorkDir: dir})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !strings.Contains(cmd, "unset CLAUDECODE") {
		t.Error("expected 'unset CLAUDECODE' in command")
	}
}

// TestBuildCommand_RejectsUnsafeArg verifies args with shell metacharacters are rejected.
func TestBuildCommand_RejectsUnsafeArg(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Args:    []string{"--flag; rm -rf /"},
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	_, err = rt.BuildCommand(SessionConfig{WorkDir: dir, Goal: "test"})
	if err == nil {
		t.Fatal("BuildCommand should reject unsafe arg")
	}
	if !strings.Contains(err.Error(), "invalid runtime arg") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewRegistry_InvalidPermissionPattern verifies error on bad permission pattern.
func TestNewRegistry_InvalidPermissionPattern(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"bad": {
			Command: "bad",
			Detection: config.RuntimeDetection{
				PermissionPattern: "[invalid",
			},
		},
	}
	_, err := NewRegistry(cfg)
	if err == nil {
		t.Fatal("expected error for invalid permission pattern")
	}
	if !strings.Contains(err.Error(), "permission pattern") {
		t.Errorf("error should mention 'permission pattern', got: %v", err)
	}
}

// TestNewRegistry_InvalidPlanModePattern verifies error on bad plan mode pattern.
func TestNewRegistry_InvalidPlanModePattern(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"bad": {
			Command: "bad",
			Detection: config.RuntimeDetection{
				PlanModePattern: "[invalid",
			},
		},
	}
	_, err := NewRegistry(cfg)
	if err == nil {
		t.Fatal("expected error for invalid plan mode pattern")
	}
	if !strings.Contains(err.Error(), "plan mode pattern") {
		t.Errorf("error should mention 'plan mode pattern', got: %v", err)
	}
}

// TestNewRegistry_EmptyConfig verifies registry with no runtimes.
func TestNewRegistry_EmptyConfig(t *testing.T) {
	reg, err := NewRegistry(map[string]config.RuntimeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected empty registry, got %d runtimes", len(reg.List()))
	}
}

// TestCLIRuntime_Name verifies the Name accessor.
func TestCLIRuntime_Name(t *testing.T) {
	rt := &CLIRuntime{name: "test-rt"}
	if rt.Name() != "test-rt" {
		t.Errorf("expected 'test-rt', got %s", rt.Name())
	}
}

// TestCLIRuntime_SupportedModels verifies the SupportedModels accessor.
func TestCLIRuntime_SupportedModels(t *testing.T) {
	rt := &CLIRuntime{models: []string{"model-a", "model-b"}}
	models := rt.SupportedModels()
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0] != "model-a" {
		t.Errorf("expected 'model-a', got %s", models[0])
	}
}

// TestBuildCommand_MultipleArgs verifies multiple args are all included.
func TestBuildCommand_MultipleArgs(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Args:    []string{"--dangerously-skip-permissions", "--verbose"},
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	cmd, err := rt.BuildCommand(SessionConfig{WorkDir: dir})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !strings.Contains(cmd, "--dangerously-skip-permissions") {
		t.Error("expected first arg in command")
	}
	if !strings.Contains(cmd, "--verbose") {
		t.Error("expected second arg in command")
	}
}

// TestBuildCommand_OnlyGoalNoSystemPrompt verifies prompt file content
// when only goal is set (no system prompt).
func TestBuildCommand_OnlyGoalNoSystemPrompt(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "claude",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	dir := t.TempDir()
	scfg := SessionConfig{
		WorkDir: dir,
		Goal:    "just the goal",
	}
	_, err = rt.BuildCommand(scfg)
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}

	promptFile := filepath.Join(dir, ".vxd-prompts", "prompt.txt")
	data, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("reading prompt file: %v", err)
	}
	if string(data) != "just the goal" {
		t.Errorf("expected 'just the goal', got %q", string(data))
	}
}

// TestSpawn_WritesCLAUDEMD verifies that Spawn writes CLAUDE.md to workdir.
// We can't test the full tmux session spawn, but we can verify the file writing.
func TestSpawn_WritesCLAUDEMD(t *testing.T) {
	dir := t.TempDir()

	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "echo",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	// Spawn will fail on tmux, but the CLAUDE.md should be written first.
	_ = rt.Spawn(SessionConfig{
		WorkDir:     dir,
		SessionName: "vxd-test-claudemd",
		Goal:        "test",
	})

	// Verify CLAUDE.md was written
	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("CLAUDE.md should have been written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "VXD Agent Directive") {
		t.Error("CLAUDE.md should contain VXD Agent Directive")
	}
	if !strings.Contains(content, "Do NOT brainstorm") {
		t.Error("CLAUDE.md should contain brainstorm directive")
	}
}

// TestSpawn_EmptyWorkDir verifies Spawn doesn't panic with empty WorkDir.
func TestSpawn_EmptyWorkDir(t *testing.T) {
	cfg := map[string]config.RuntimeConfig{
		"test": {
			Command: "echo",
			Detection: config.RuntimeDetection{
				IdlePattern: `\$`,
			},
		},
	}
	reg, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	rtIface, _ := reg.Get("test")
	rt := rtIface.(*CLIRuntime)

	// Should not panic with empty WorkDir
	_ = rt.Spawn(SessionConfig{
		SessionName: "vxd-test-empty-workdir",
		Goal:        "test",
	})
}

// TestTerminate_NonExistentSession verifies Terminate error on missing session.
func TestTerminate_NonExistentSession(t *testing.T) {
	rt := &CLIRuntime{name: "test"}
	err := rt.Terminate("vxd-nonexistent-session-xyz")
	if err == nil {
		t.Error("Terminate on non-existent session should return error")
	}
}

// TestSendInput_NonExistentSession verifies SendInput error on missing session.
func TestSendInput_NonExistentSession(t *testing.T) {
	rt := &CLIRuntime{name: "test"}
	err := rt.SendInput("vxd-nonexistent-session-xyz", "hello")
	if err == nil {
		t.Error("SendInput on non-existent session should return error")
	}
}

// TestReadOutput_NonExistentSession verifies ReadOutput error on missing session.
func TestReadOutput_NonExistentSession(t *testing.T) {
	rt := &CLIRuntime{name: "test"}
	_, err := rt.ReadOutput("vxd-nonexistent-session-xyz", 10)
	if err == nil {
		t.Error("ReadOutput on non-existent session should return error")
	}
}

// TestDetectStatus_TerminatedSession verifies StatusTerminated for missing session.
func TestDetectStatus_TerminatedSession(t *testing.T) {
	rt := &CLIRuntime{name: "test"}
	status, err := rt.DetectStatus("vxd-nonexistent-session-xyz")
	if err != nil {
		t.Fatalf("DetectStatus should not error for terminated session: %v", err)
	}
	if status != StatusTerminated {
		t.Errorf("expected StatusTerminated, got %s", status)
	}
}
