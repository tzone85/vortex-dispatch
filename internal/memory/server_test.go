package memory

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// newTestServer creates a Server backed by a temp dir with test data.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	// Write changelog data
	entries := []changelogEntry{
		{
			RunID:          "2026-04-08T14:00:00Z",
			FindingID:      "f-001",
			Title:          "Go Vuln DB",
			Category:       "security",
			Source:         "https://vuln.go.dev/",
			Relevance:      9,
			Impact:         5,
			Risk:           2,
			Disposition:    "proposed",
			Reasoning:      "Important for Go security",
			TestsPassed:    true,
			SecurityReview: "Automated check",
			LicenseCheck:   "pass",
		},
		{
			RunID:        "2026-04-08T14:00:00Z",
			FindingID:    "f-002",
			Title:        "PR Finding",
			Category:     "tooling",
			Source:       "https://example.com",
			Relevance:    7,
			Disposition:  "merged",
			PRURL:        "https://github.com/test/pr/42",
			Lines:        150,
			LicenseCheck: "pass",
		},
	}

	changelogPath := filepath.Join(dir, "changelog.jsonl")
	f, err := os.Create(changelogPath)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, e := range entries {
		enc.Encode(e) //nolint:errcheck
	}
	f.Close()

	// Write run summary
	runsDir := filepath.Join(dir, "runs")
	os.MkdirAll(runsDir, 0o755) //nolint:errcheck
	runData, _ := json.Marshal(runSummary{
		SourcesScraped:   11,
		FindingsTotal:    11,
		FindingsRelevant: 5,
		PRsCreated:       1,
		EmailSent:        true,
	})
	os.WriteFile(filepath.Join(runsDir, "2026-04-08.json"), runData, 0o644) //nolint:errcheck

	return NewServer(dir, dir, 0)
}

func TestNewServer(t *testing.T) {
	s := NewServer("/tmp/audit", "/tmp/repo", 8078)
	if s.auditDir != "/tmp/audit" {
		t.Errorf("auditDir = %q, want /tmp/audit", s.auditDir)
	}
	if s.repoDir != "/tmp/repo" {
		t.Errorf("repoDir = %q, want /tmp/repo", s.repoDir)
	}
	if s.port != 8078 {
		t.Errorf("port = %d, want 8078", s.port)
	}
}

func TestMarshalInit(t *testing.T) {
	s := newTestServer(t)

	data, err := s.MarshalInit()
	if err != nil {
		t.Fatalf("MarshalInit: %v", err)
	}

	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type != "init" {
		t.Errorf("Type = %q, want init", msg.Type)
	}
	if msg.Range == nil {
		t.Fatal("Range is nil")
	}
	if msg.Range.Min == "" {
		t.Error("Range.Min is empty")
	}
	if len(msg.Timeline) == 0 {
		t.Error("Timeline is empty")
	}
}

func TestWebSocket_InitMessage(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	var msg ServerMessage
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("Read init: %v", err)
	}
	if msg.Type != "init" {
		t.Errorf("expected init message, got %q", msg.Type)
	}
	if msg.Range == nil {
		t.Fatal("Range is nil in init")
	}
	if msg.Range.Today == "" {
		t.Error("Today should be set")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_SelectDate(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	// Read init message first
	var initMsg ServerMessage
	if err := wsjson.Read(ctx, conn, &initMsg); err != nil {
		t.Fatalf("Read init: %v", err)
	}

	// Send select_date
	selectMsg := ClientMessage{Type: "select_date", Date: "2026-04-08"}
	if err := wsjson.Write(ctx, conn, selectMsg); err != nil {
		t.Fatalf("Write select_date: %v", err)
	}

	// Read day_detail response
	var dayMsg ServerMessage
	if err := wsjson.Read(ctx, conn, &dayMsg); err != nil {
		t.Fatalf("Read day_detail: %v", err)
	}
	if dayMsg.Type != "day_detail" {
		t.Errorf("expected day_detail, got %q", dayMsg.Type)
	}
	if dayMsg.Date != "2026-04-08" {
		t.Errorf("expected date=2026-04-08, got %q", dayMsg.Date)
	}
	if len(dayMsg.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(dayMsg.Findings))
	}
	if len(dayMsg.PRs) != 1 {
		t.Errorf("expected 1 PR, got %d", len(dayMsg.PRs))
	}
	if dayMsg.RunSummary == nil {
		t.Error("expected run summary")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_ListFindings(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	var initMsg ServerMessage
	if err := wsjson.Read(ctx, conn, &initMsg); err != nil {
		t.Fatalf("Read init: %v", err)
	}

	if err := wsjson.Write(ctx, conn, ClientMessage{Type: "list_findings"}); err != nil {
		t.Fatalf("Write list_findings: %v", err)
	}

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read findings_library: %v", err)
	}

	if resp.Type != "findings_library" {
		t.Fatalf("expected findings_library, got %q", resp.Type)
	}
	if len(resp.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(resp.Findings))
	}
	if resp.Findings[0].FindingID != "f-001" {
		t.Errorf("expected highest-ranked finding first, got %q", resp.Findings[0].FindingID)
	}
	if !resp.Findings[0].TestsPassed {
		t.Error("expected tests_passed to be preserved")
	}
	if resp.Findings[0].LicenseCheck != "pass" {
		t.Errorf("license_check = %q, want pass", resp.Findings[0].LicenseCheck)
	}
}

func TestWebSocket_Search(t *testing.T) {
	// Inject a deterministic searchFunc so the test does not depend on the
	// real `mempalace` binary or its index size. Without this the handler
	// can take longer than the test deadline on a primed dev box.
	withSearchFunc(t, func(ctx context.Context, query string) ([]SearchResult, error) {
		return []SearchResult{{Wing: "test", Room: "ws", Text: query, Similarity: 0.9}}, nil
	})

	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	// Read init
	var initMsg ServerMessage
	wsjson.Read(ctx, conn, &initMsg) //nolint:errcheck

	searchMsg := ClientMessage{Type: "search", Query: "security", Date: "2026-04-08"}
	if err := wsjson.Write(ctx, conn, searchMsg); err != nil {
		t.Fatalf("Write search: %v", err)
	}

	var searchResp ServerMessage
	if err := wsjson.Read(ctx, conn, &searchResp); err != nil {
		t.Fatalf("Read search_results: %v", err)
	}
	if searchResp.Type != "search_results" {
		t.Errorf("expected search_results, got %q", searchResp.Type)
	}
	if searchResp.Query != "security" {
		t.Errorf("expected query=security, got %q", searchResp.Query)
	}
	if len(searchResp.Results) != 1 {
		t.Errorf("expected 1 injected result, got %d", len(searchResp.Results))
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

// TestWebSocket_Search_SlowSearchRespectsTimeout pins the regression: when the
// underlying MemPalace search blocks past the handler's bound, the WebSocket
// client still receives a `search_results` frame (with empty results) — it does
// not hang until the client's own deadline.
func TestWebSocket_Search_SlowSearchRespectsTimeout(t *testing.T) {
	withSearchFunc(t, func(ctx context.Context, query string) ([]SearchResult, error) {
		// Block until the handler cancels searchCtx (searchTimeout), then
		// return ctx.Err — mimics a stuck mempalace process.
		<-ctx.Done()
		return nil, ctx.Err()
	})

	s := newTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	var initMsg ServerMessage
	wsjson.Read(ctx, conn, &initMsg) //nolint:errcheck

	start := time.Now()
	if err := wsjson.Write(ctx, conn, ClientMessage{Type: "search", Query: "anything"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}
	elapsed := time.Since(start)

	if resp.Type != "search_results" {
		t.Errorf("expected search_results, got %q", resp.Type)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results on timeout, got %d", len(resp.Results))
	}
	// Handler bound is searchTimeout (2s); allow generous slack for CI scheduler.
	if elapsed > 4*time.Second {
		t.Errorf("handler did not honor searchTimeout: elapsed %v", elapsed)
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

// withSearchFunc swaps the package-level searchFunc for the duration of the test
// and restores it on cleanup. Not safe for parallel use of the same package var.
func withSearchFunc(t *testing.T, fn func(context.Context, string) ([]SearchResult, error)) {
	t.Helper()
	original := searchFunc
	searchFunc = fn
	t.Cleanup(func() { searchFunc = original })
}

func TestWebSocket_SelectDate_NoData(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	// Read init
	var initMsg ServerMessage
	wsjson.Read(ctx, conn, &initMsg) //nolint:errcheck

	// Select a date with no data
	selectMsg := ClientMessage{Type: "select_date", Date: "2020-01-01"}
	wsjson.Write(ctx, conn, selectMsg) //nolint:errcheck

	var dayMsg ServerMessage
	if err := wsjson.Read(ctx, conn, &dayMsg); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if dayMsg.Type != "day_detail" {
		t.Errorf("expected day_detail, got %q", dayMsg.Type)
	}
	if len(dayMsg.Findings) != 0 {
		t.Errorf("expected 0 findings for empty date, got %d", len(dayMsg.Findings))
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestHandler_StaticFiles(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Test that index.html is served
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
}
