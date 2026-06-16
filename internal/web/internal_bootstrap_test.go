package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalBootstrap_LoopbackPostMintsFreshNonce(t *testing.T) {
	s := &Server{}
	// Hand-build an authenticator so the rotator surface is live without
	// spinning a real Server.Start.
	_, rotator := NewAuthMiddlewareWithRotator(AuthOptions{
		Token:          "tok",
		BootstrapNonce: "old-nonce",
	})
	s.rotator = rotator

	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()

	s.internalBootstrapHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	nonce := body["bootstrap"]
	if nonce == "" || nonce == "old-nonce" {
		t.Errorf("got nonce=%q, want fresh 32-byte hex", nonce)
	}
	if len(nonce) != 64 {
		t.Errorf("nonce length = %d, want 64 (hex of 32 bytes)", len(nonce))
	}
}

func TestInternalBootstrap_RejectsNonPost(t *testing.T) {
	s := &Server{}
	_, rotator := NewAuthMiddlewareWithRotator(AuthOptions{Token: "tok"})
	s.rotator = rotator

	req := httptest.NewRequest(http.MethodGet, "/internal/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()

	s.internalBootstrapHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestInternalBootstrap_RejectsRemoteHosts(t *testing.T) {
	s := &Server{}
	_, rotator := NewAuthMiddlewareWithRotator(AuthOptions{Token: "tok"})
	s.rotator = rotator

	for _, addr := range []string{"10.0.0.1:54321", "192.168.1.10:1234", "203.0.113.5:443"} {
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()

		s.internalBootstrapHandler(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("addr=%s: status = %d, want 403", addr, w.Code)
		}
		if !strings.Contains(w.Body.String(), "forbidden") {
			t.Errorf("addr=%s: body = %q, want 'forbidden'", addr, w.Body.String())
		}
	}
}

func TestInternalBootstrap_NoRotatorReturns503(t *testing.T) {
	s := &Server{} // rotator left nil
	req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()

	s.internalBootstrapHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
