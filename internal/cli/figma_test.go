package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/figma"
)

// newFigmaFixture serves /v1/me for a known-good token.
func newFigmaFixture(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "figd_valid" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1","email":"op@example.com","handle":"Operator"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFigmaAuth_ValidatesAndStoresToken(t *testing.T) {
	srv := newFigmaFixture(t)
	figmaAPIBase = srv.URL
	t.Cleanup(func() { figmaAPIBase = "" })

	cmd := newFigmaAuthCmd()
	out := driveWithVxdYaml(t, cmd)
	cmd.SetIn(strings.NewReader("figd_valid\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"one-time interactive session", // the UX promise: interactive ONCE
		"figma.com/settings",           // URL printed, never auto-opened
		"Operator",                     // validated identity echoed back
		"fire-and-forget",              // and back to autonomous runs
	} {
		if !strings.Contains(got, want) {
			t.Errorf("auth output missing %q:\n%s", want, got)
		}
	}
}

func TestFigmaAuth_RejectsInvalidTokenWithoutStoring(t *testing.T) {
	srv := newFigmaFixture(t)
	figmaAPIBase = srv.URL
	t.Cleanup(func() { figmaAPIBase = "" })

	cmd := newFigmaAuthCmd()
	driveWithVxdYaml(t, cmd)
	cmd.SetIn(strings.NewReader("figd_typo\n"))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not stored") {
		t.Fatalf("invalid token must fail loudly in the interactive session, got %v", err)
	}
}

func TestFigmaAuth_EmptyInput(t *testing.T) {
	cmd := newFigmaAuthCmd()
	driveWithVxdYaml(t, cmd)
	cmd.SetIn(bytes.NewReader([]byte("\n")))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no token entered") {
		t.Fatalf("empty input must be rejected, got %v", err)
	}
}

func TestFigmaStatus_NotConfigured(t *testing.T) {
	t.Setenv(figma.TokenEnvVar, "")
	cmd := newFigmaStatusCmd()
	out := driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status must not error when unconfigured: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "not configured") || !strings.Contains(got, "vxd figma auth") {
		t.Errorf("status must explain the interactive step:\n%s", got)
	}
}

func TestFigmaStatus_Authenticated(t *testing.T) {
	srv := newFigmaFixture(t)
	figmaAPIBase = srv.URL
	t.Cleanup(func() { figmaAPIBase = "" })
	t.Setenv(figma.TokenEnvVar, "figd_valid")

	cmd := newFigmaStatusCmd()
	out := driveWithVxdYaml(t, cmd)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "authenticated as Operator") {
		t.Errorf("status must show the account:\n%s", out.String())
	}
}
