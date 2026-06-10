package web

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// staticSubFS returns the embedded static file subtree used by the server.
func staticSubFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	return sub
}

// TestWebSocket_RejectsMissingToken verifies the command channel refuses
// connections without the session token (closing the unauthenticated-control
// and no-Origin local-process bypass).
func TestWebSocket_RejectsMissingToken(t *testing.T) {
	s := newTestServer(t)
	s.authToken = "secret-token" // enable auth for this test

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.hub.HandleWebSocket)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// No token → must be rejected (401), so Dial fails.
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/ws", nil)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected connection without token to be rejected")
	}
}

func TestWebSocket_AcceptsValidToken(t *testing.T) {
	s := newTestServer(t)
	s.authToken = "secret-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.hub.HandleWebSocket)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/ws?token=secret-token", nil)
	if err != nil {
		t.Fatalf("expected connection with valid token to succeed: %v", err)
	}
	conn.CloseNow()
}

func TestWebSocket_RejectsWrongToken(t *testing.T) {
	s := newTestServer(t)
	s.authToken = "secret-token"

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.hub.HandleWebSocket)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/ws?token=wrong", nil)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected connection with wrong token to be rejected")
	}
}

// TestServeIndex_DoesNotLeakTokenWithoutToken verifies that fetching "/"
// without the token does not reveal the secret — preventing a local process
// from harvesting it.
func TestServeIndex_DoesNotLeakTokenWithoutToken(t *testing.T) {
	s := newTestServer(t)
	s.authToken = "super-secret"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.serveIndex(rec, req, staticSubFS(t))

	body := rec.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Error("index served without a token must NOT contain the session token")
	}
	if !strings.Contains(body, "window.__VXD_TOKEN__") {
		t.Error("index should still inject an (empty) token variable")
	}
}

func TestServeIndex_InjectsTokenWhenAuthorized(t *testing.T) {
	s := newTestServer(t)
	s.authToken = "super-secret"

	req := httptest.NewRequest(http.MethodGet, "/?token=super-secret", nil)
	rec := httptest.NewRecorder()
	s.serveIndex(rec, req, staticSubFS(t))

	if !strings.Contains(rec.Body.String(), "super-secret") {
		t.Error("authorized index load should inject the session token")
	}
	// And it should set the persistence cookie.
	if len(rec.Result().Cookies()) == 0 {
		t.Error("authorized index load should set the session cookie")
	}
}
