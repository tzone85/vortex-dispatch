package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestParseBudget_EmptyDefault(t *testing.T) {
	got := parseBudget("")
	if got != 5*time.Minute {
		t.Errorf("got %v, want 5m default", got)
	}
}

func TestParseBudget_InvalidString(t *testing.T) {
	got := parseBudget("not-a-duration")
	if got != 5*time.Minute {
		t.Errorf("invalid input should fall back to 5m, got %v", got)
	}
}

func TestParseBudget_NonPositive(t *testing.T) {
	for _, in := range []string{"0s", "-3m"} {
		t.Run(in, func(t *testing.T) {
			got := parseBudget(in)
			if got != 5*time.Minute {
				t.Errorf("non-positive %q should fall back to 5m, got %v", in, got)
			}
		})
	}
}

func TestParseBudget_ValidValues(t *testing.T) {
	cases := map[string]time.Duration{
		"1m":    time.Minute,
		"45s":   45 * time.Second,
		"2h":    2 * time.Hour,
		"500ms": 500 * time.Millisecond,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := parseBudget(in); got != want {
				t.Errorf("parseBudget(%q) = %v, want %v", in, got, want)
			}
		})
	}
}

func TestRepoLabel(t *testing.T) {
	if got := repoLabel(""); got != "(all repos)" {
		t.Errorf("empty repo got %q, want (all repos)", got)
	}
	if got := repoLabel("foo/bar"); got != "foo/bar" {
		t.Errorf("named repo got %q, want foo/bar", got)
	}
}

func TestAutoresearchShortID(t *testing.T) {
	if got := autoresearchShortID("abc"); got != "abc" {
		t.Errorf("short id passthrough got %q", got)
	}
	if got := autoresearchShortID("0123456789abcdef"); got != "01234567" {
		t.Errorf("long id should truncate to 8, got %q", got)
	}
	if got := autoresearchShortID(""); got != "" {
		t.Errorf("empty id passthrough got %q", got)
	}
}

func TestGuessLanguage_GoMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := guessLanguage(dir); got != "go" {
		t.Errorf("got %q, want go", got)
	}
}

func TestGuessLanguage_JavascriptMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := guessLanguage(dir); got != "javascript" {
		t.Errorf("got %q, want javascript", got)
	}
}

func TestGuessLanguage_NoMarkers(t *testing.T) {
	if got := guessLanguage(t.TempDir()); got != "" {
		t.Errorf("empty dir should return empty label, got %q", got)
	}
}

func TestGuessLanguage_OrderPrefersGoOverJS(t *testing.T) {
	// guessLanguage walks the checks slice in declaration order. Drop
	// markers for both Go and JS — the Go branch is checked first.
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "package.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(""), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if got := guessLanguage(dir); got != "go" {
		t.Errorf("priority order: got %q, want go (first match)", got)
	}
}

func TestLookPath_FindsKnownBinary(t *testing.T) {
	// Use a binary that's universally present.
	candidate := "ls"
	if runtime.GOOS == "windows" {
		candidate = "cmd.exe"
	}
	got, err := lookPath(candidate)
	if err != nil {
		t.Fatalf("lookPath(%s): %v", candidate, err)
	}
	if !strings.Contains(got, candidate) {
		t.Errorf("resolved path %q does not contain %q", got, candidate)
	}
}

func TestLookPath_MissingBinary(t *testing.T) {
	_, err := lookPath("definitely-not-a-real-binary-vxd-xyz")
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestDefaultStateDir_RespectsEnv(t *testing.T) {
	// Save + restore env so we don't leak to other tests.
	prev, had := os.LookupEnv("VXD_STATE_DIR")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("VXD_STATE_DIR", prev)
		} else {
			_ = os.Unsetenv("VXD_STATE_DIR")
		}
	})

	custom := "/tmp/vxd-override"
	_ = os.Setenv("VXD_STATE_DIR", custom)
	if got := defaultStateDir(); got != custom {
		t.Errorf("VXD_STATE_DIR not honoured: got %q, want %q", got, custom)
	}
}

func TestPickAutoresearchRuntime_EmptyConfig(t *testing.T) {
	_, _, err := pickAutoresearchRuntime(nil)
	if err == nil {
		t.Error("empty runtime config should error")
	}
}

func TestPickAutoresearchRuntime_InvalidDetectionRegex(t *testing.T) {
	cfgMap := map[string]config.RuntimeConfig{
		"bad": {Command: "x", Detection: config.RuntimeDetection{IdlePattern: "[broken"}},
	}
	_, _, err := pickAutoresearchRuntime(cfgMap)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestPickAutoresearchRuntime_PicksAlphabeticallyFirst(t *testing.T) {
	cfgMap := map[string]config.RuntimeConfig{
		"zeta":  {Command: "z"},
		"alpha": {Command: "a"},
		"mu":    {Command: "m"},
	}
	rt, name, err := pickAutoresearchRuntime(cfgMap)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if rt == nil || name != "alpha" {
		t.Errorf("got name %q, want alpha", name)
	}
}

func TestBuildAutoresearchLLMClient_NoCodexCLI(t *testing.T) {
	prevPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prevPath) })
	_ = os.Setenv("PATH", "/no/such/dir")

	_, err := buildAutoresearchLLMClient(emptyConfig())
	if err == nil {
		t.Error("expected error when the codex CLI is unavailable")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("error should explain the required GPT path, got %v", err)
	}
}

func TestBuildAutoresearchLLMClient_WithCodexCLI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	prevPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", prevPath) })
	_ = os.Setenv("PATH", dir)

	client, err := buildAutoresearchLLMClient(emptyConfig())
	if err != nil {
		t.Fatalf("build with Codex CLI: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil GPT client when Codex CLI is present")
	}
}

func emptyConfig() config.Config {
	return config.Config{}
}

func TestDefaultStateDir_HonoursOverride(t *testing.T) {
	prev := stateDirOverride
	stateDirOverride = "/tmp/test-state-override"
	defer func() { stateDirOverride = prev }()
	if got := defaultStateDir(); got != "/tmp/test-state-override" {
		t.Errorf("override ignored: got %q", got)
	}
}

func TestDefaultStateDir_FallsBackToHome(t *testing.T) {
	prev, had := os.LookupEnv("VXD_STATE_DIR")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("VXD_STATE_DIR", prev)
		} else {
			_ = os.Unsetenv("VXD_STATE_DIR")
		}
	})
	_ = os.Unsetenv("VXD_STATE_DIR")
	got := defaultStateDir()
	if got == "" {
		t.Error("expected non-empty default state dir")
	}
	// Either ~/.vxd or the literal ".vxd" fallback.
	if !strings.HasSuffix(got, ".vxd") {
		t.Errorf("expected suffix .vxd, got %q", got)
	}
}
