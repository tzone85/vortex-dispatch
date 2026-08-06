package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── LoadOrGenerateToken ─────────────────────────────────────────────────

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

// ─── NewAuthMiddleware: panic-safe + AllowUnauthenticated ────────────────

func TestNewAuthMiddleware_PanicsOnEmptyToken(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty token without AllowUnauthenticated, got none")
		}
	}()
	_ = NewAuthMiddleware(AuthOptions{Token: ""})
}

func TestNewAuthMiddleware_AllowUnauthenticatedExplicit(t *testing.T) {
	mw := NewAuthMiddleware(AuthOptions{AllowUnauthenticated: true})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		t.Errorf("AllowUnauthenticated: status = %d, want 200", resp.StatusCode)
	}
}

// ─── RequireToken — bypass paths + missing/wrong token ───────────────────

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

// ─── Bearer header path ──────────────────────────────────────────────────

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

// ─── Cookie path ─────────────────────────────────────────────────────────

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

// ─── Single-use bootstrap nonce ──────────────────────────────────────────

func newNonceMiddleware(token, nonce string) http.Handler {
	mw := NewAuthMiddleware(AuthOptions{Token: token, BootstrapNonce: nonce})
	return mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestBootstrapNonce_FirstUseSetsCookieAndSucceeds(t *testing.T) {
	srv := httptest.NewServer(newNonceMiddleware("the-token", "one-shot-nonce"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/?bootstrap=one-shot-nonce")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("first bootstrap: status = %d, want 200", resp.StatusCode)
	}

	var foundCookie bool
	for _, c := range resp.Cookies() {
		if c.Name == TokenCookieName && c.Value == "the-token" {
			foundCookie = true
			if !c.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("session cookie SameSite = %v, want Strict", c.SameSite)
			}
			// Secure is conditional on TLS / X-Forwarded-Proto=https.
			// This test uses a plain-HTTP httptest server, so Secure
			// MUST be false — setting it would break the auth flow on
			// any subsequent request (the browser drops Secure cookies
			// over plain HTTP). See TestBootstrapNonce_SecureCookieOver-
			// ForwardedHTTPS for the proxied case.
			if c.Secure {
				t.Error("session cookie must not be Secure on a plain-HTTP listener — would break the auth flow")
			}
		}
	}
	if !foundCookie {
		t.Error("expected cookie set after first bootstrap")
	}
}

// TestBootstrapNonce_SecureCookieOverForwardedHTTPS asserts the cookie
// becomes Secure when the request reports HTTPS via X-Forwarded-Proto —
// the reverse-proxy scenario the audit flagged. Without this, a
// Mixed-Content config would leak the token over plain HTTP.
func TestBootstrapNonce_SecureCookieOverForwardedHTTPS(t *testing.T) {
	srv := httptest.NewServer(newNonceMiddleware("tok", "nonce"))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/?bootstrap=nonce", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == TokenCookieName {
			if !c.Secure {
				t.Error("Secure must be true when X-Forwarded-Proto=https")
			}
		}
	}
}

// TestAuthHeaders_SetsSecurityBaseline confirms the OWASP-baseline
// response headers are present on every request — including unauth'd
// rejections — so a browser-facing operator gets clickjack / MIME-
// sniff / CSP protection regardless of which endpoint they hit.
func TestAuthHeaders_SetsSecurityBaseline(t *testing.T) {
	srv := httptest.NewServer(newNonceMiddleware("tok", "nonce"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/requirements")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, want := range []struct {
		key, value string
	}{
		{"Referrer-Policy", "no-referrer"},
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
	} {
		if got := resp.Header.Get(want.key); got != want.value {
			t.Errorf("%s = %q, want %q", want.key, got, want.value)
		}
	}
	wantCSP := "default-src 'self'; img-src 'self' data:; media-src 'self' https://d8j0ntlcm91z4.cloudfront.net; connect-src 'self' ws: wss:; frame-ancestors 'none'"
	if got := resp.Header.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
}

func TestRedactTokenForLog_TruncatesLongTokens(t *testing.T) {
	got := redactTokenForLog("0123456789abcdef0123456789abcdef")
	if got != "01234567" {
		t.Errorf("redactTokenForLog long = %q, want 01234567 (8-char prefix)", got)
	}
	if got := redactTokenForLog("short"); got != "short" {
		t.Errorf("redactTokenForLog short = %q, want short (passthrough)", got)
	}
	if got := redactTokenForLog(""); got != "" {
		t.Errorf("redactTokenForLog empty = %q, want empty", got)
	}
}

func TestBootstrapNonce_SecondUseFails(t *testing.T) {
	srv := httptest.NewServer(newNonceMiddleware("the-token", "one-shot-nonce"))
	defer srv.Close()

	first, err := http.Get(srv.URL + "/?bootstrap=one-shot-nonce")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first bootstrap: status = %d", first.StatusCode)
	}

	// Replay the same URL — clean client, no cookie carried over.
	second, err := http.Get(srv.URL + "/?bootstrap=one-shot-nonce")
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusUnauthorized {
		t.Errorf("replayed bootstrap: status = %d, want 401", second.StatusCode)
	}
}

func TestBootstrapNonce_WrongNonceFails(t *testing.T) {
	srv := httptest.NewServer(newNonceMiddleware("the-token", "real-nonce"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/?bootstrap=wrong-nonce")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong nonce: status = %d, want 401", resp.StatusCode)
	}
}

// ─── Referrer-Policy header on every authenticated response ──────────────

func TestRequireToken_SetsReferrerPolicy(t *testing.T) {
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
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}
