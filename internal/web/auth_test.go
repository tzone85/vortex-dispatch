package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrGenerateToken_EnvOverride(t *testing.T) {
	t.Setenv("VXD_DASHBOARD_TOKEN", "from-env-override")
	tok, err := LoadOrGenerateToken(filepath.Join(t.TempDir(), "tok"))
	if err != nil {
		t.Fatalf("LoadOrGenerateToken: %v", err)
	}
	if tok != "from-env-override" {
		t.Errorf("token = %q, want from-env-override", tok)
	}
}

func TestLoadOrGenerateToken_ReadsExistingFile(t *testing.T) {
	os.Unsetenv("VXD_DASHBOARD_TOKEN")
	dir := t.TempDir()
	path := filepath.Join(dir, "tok")
	if err := os.WriteFile(path, []byte("on-disk-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadOrGenerateToken(path)
	if err != nil {
		t.Fatalf("LoadOrGenerateToken: %v", err)
	}
	if tok != "on-disk-token" {
		t.Errorf("token = %q, want on-disk-token", tok)
	}
}

func TestLoadOrGenerateToken_GeneratesAndPersists(t *testing.T) {
	os.Unsetenv("VXD_DASHBOARD_TOKEN")
	dir := t.TempDir()
	path := filepath.Join(dir, "tok")

	tok, err := LoadOrGenerateToken(path)
	if err != nil {
		t.Fatalf("LoadOrGenerateToken: %v", err)
	}
	if len(tok) != 64 { // 32 bytes hex-encoded.
		t.Errorf("expected 64-char hex token, got %d chars", len(tok))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("token file mode = %o, want 0600", mode)
	}

	// Second call returns same token (re-read).
	tok2, err := LoadOrGenerateToken(path)
	if err != nil {
		t.Fatalf("second LoadOrGenerateToken: %v", err)
	}
	if tok2 != tok {
		t.Errorf("second call returned different token: %q vs %q", tok2, tok)
	}
}

func TestRequireToken_AllowsBypassPaths(t *testing.T) {
	h := RequireToken("the-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/health status = %d, want 200", resp.StatusCode)
	}
}

func TestRequireToken_RejectsMissingAndWrongToken(t *testing.T) {
	h := RequireToken("the-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Missing entirely.
	resp, err := http.Get(srv.URL + "/api/v1/requirements")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("expected WWW-Authenticate Bearer hint, got %q", resp.Header.Get("WWW-Authenticate"))
	}

	// Wrong header.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/requirements", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
	}
}

func TestRequireToken_AcceptsBearerHeader(t *testing.T) {
	h := RequireToken("the-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/requirements", nil)
	req.Header.Set("Authorization", "Bearer the-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer token: status = %d, want 200", resp.StatusCode)
	}
}

func TestRequireToken_AcceptsQueryParamAndSetsCookie(t *testing.T) {
	h := RequireToken("the-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/?token=the-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("query token: status = %d, want 200", resp.StatusCode)
	}
	var foundCookie bool
	for _, c := range resp.Cookies() {
		if c.Name == TokenCookieName && c.Value == "the-token" {
			foundCookie = true
		}
	}
	if !foundCookie {
		t.Errorf("expected %s cookie set on successful query-token auth", TokenCookieName)
	}
}

func TestRequireToken_AcceptsCookie(t *testing.T) {
	h := RequireToken("the-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/requirements", nil)
	req.AddCookie(&http.Cookie{Name: TokenCookieName, Value: "the-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("cookie auth: status = %d, want 200", resp.StatusCode)
	}
}

func TestRequireToken_EmptyTokenDisablesAuth(t *testing.T) {
	// Defensive escape hatch — empty token means no auth check. Intended
	// for "you got it wrong in main, don't lock yourself out" diagnostics.
	h := RequireToken("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/requirements")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("empty token: status = %d, want 200", resp.StatusCode)
	}
}
