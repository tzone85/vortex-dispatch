package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestHealthHandler_ReturnsOK(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)

	healthHandler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("status field = %q, want %q", resp["status"], "ok")
	}
	// The bare-function healthHandler still includes version (it's the
	// legacy form). The Server.healthHandler — exposed at /health on the
	// real server — does NOT include version; see
	// TestServer_HealthHandlerEndToEnd_OmitsTelemetry.
}

func TestHealthHandler_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)

	healthHandler(w, r)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestBuildHealthResponse_NilStore(t *testing.T) {
	resp := buildHealthResponse(nil, time.Now().Add(-5*time.Second))
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
	if uptime, ok := resp["uptime_seconds"].(int); !ok || uptime < 5 {
		t.Errorf("uptime_seconds = %v, want >= 5", resp["uptime_seconds"])
	}
	if _, has := resp["events_total"]; has {
		t.Error("events_total should be absent when store is nil")
	}
}

func TestBuildHealthResponse_WithStore(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()

	// Append a few events
	for i := 0; i < 3; i++ {
		es.Append(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"id": i}))
	}

	resp := buildHealthResponse(es, time.Now().Add(-1*time.Second))
	total, ok := resp["events_total"].(int)
	if !ok || total != 3 {
		t.Errorf("events_total = %v, want 3", resp["events_total"])
	}
}

// TestServer_HealthHandlerEndToEnd_OmitsTelemetry pins the security
// hardening that strips uptime / events_total / version from the
// UNAUTHENTICATED /health response. Operational telemetry must move to
// authenticated endpoints; /health stays minimal so an unauth visitor
// cannot fingerprint the deployment or enumerate event counts.
func TestServer_HealthHandlerEndToEnd_OmitsTelemetry(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	es.Append(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"id": 1}))

	srv := NewServer(es, ps, 0, state.ReqFilter{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	srv.healthHandler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
	for _, banned := range []string{"uptime_seconds", "events_total", "version"} {
		if _, ok := resp[banned]; ok {
			t.Errorf("/health unauthenticated response must NOT include %q (telemetry leak)", banned)
		}
	}
}
