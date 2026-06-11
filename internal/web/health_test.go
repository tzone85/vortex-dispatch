package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// The bare `healthHandler` function and `buildHealthResponse` helper
// were removed 2026-06-11 — both leaked version / uptime / events_total
// but were never registered on any mux. The only live path is
// `Server.healthHandler`, exercised by
// `TestServer_HealthHandlerEndToEnd_OmitsTelemetry` below.

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
