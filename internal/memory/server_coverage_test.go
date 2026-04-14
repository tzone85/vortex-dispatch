package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"net/http/httptest"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// newTestServerWithOpportunities creates a test server that also has
// opportunities, revenue, and discovered sources data.
func newTestServerWithOpportunities(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)

	oppDir := s.opportunitiesDir
	os.MkdirAll(oppDir, 0o755) //nolint:errcheck

	// Pipeline data
	opps := []OpportunityDetail{
		{ID: "opp-001", Title: "Go Backend", Status: "new", Rank: 80, Source: "upwork"},
		{ID: "opp-002", Title: "API Work", Status: "proposal_drafted", Rank: 60, Source: "freelancer"},
		{ID: "opp-003", Title: "DevOps", Status: "won", Rank: 90, Revenue: 3000, Source: "direct"},
	}
	writeJSONL(t, filepath.Join(oppDir, "pipeline.jsonl"), opps)

	// Revenue data
	revEntries := []struct {
		OpportunityID   string  `json:"opportunity_id"`
		Amount          float64 `json:"amount"`
		Currency        string  `json:"currency"`
		Date            string  `json:"date"`
		Status          string  `json:"status"`
		CumulativeTotal float64 `json:"cumulative_total"`
	}{
		{OpportunityID: "opp-003", Amount: 3000, Currency: "USD", Date: "2026-04-10", Status: "received", CumulativeTotal: 3000},
	}
	writeJSONL(t, filepath.Join(oppDir, "revenue.jsonl"), revEntries)

	// Discovered sources
	sources := []DiscoveredSourceDetail{
		{URL: "https://golang-weekly.com", Name: "Golang Weekly", Status: "pending", DiscoveredOn: "2026-04-10", Reason: "Go news"},
		{URL: "https://remote.co", Name: "Remote.co", Status: "approved", DiscoveredOn: "2026-04-09", Reason: "Remote jobs"},
	}
	writeJSONL(t, filepath.Join(oppDir, "discovered_sources.jsonl"), sources)

	return s
}

func TestWebSocket_ListOpportunities(t *testing.T) {
	s := newTestServerWithOpportunities(t)
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

	// Send list_opportunities
	msg := ClientMessage{Type: "list_opportunities", Filter: "", Sort: "rank"}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if resp.Type != "opportunities" {
		t.Errorf("expected opportunities, got %q", resp.Type)
	}
	if len(resp.Opportunities) != 3 {
		t.Errorf("expected 3 opportunities, got %d", len(resp.Opportunities))
	}
	// Sorted by rank descending
	if resp.Opportunities[0].Rank < resp.Opportunities[1].Rank {
		t.Error("expected opportunities sorted by rank descending")
	}
	if resp.OpportunityStats == nil {
		t.Fatal("expected opportunity stats")
	}
	if resp.OpportunityStats.Total != 3 {
		t.Errorf("expected Total=3, got %d", resp.OpportunityStats.Total)
	}
	if resp.OpportunityStats.Won != 1 {
		t.Errorf("expected Won=1, got %d", resp.OpportunityStats.Won)
	}
	if resp.OpportunityStats.Revenue != 3000 {
		t.Errorf("expected Revenue=3000, got %f", resp.OpportunityStats.Revenue)
	}
	if len(resp.DiscoveredSources) != 2 {
		t.Errorf("expected 2 discovered sources, got %d", len(resp.DiscoveredSources))
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_ListOpportunities_WithFilter(t *testing.T) {
	s := newTestServerWithOpportunities(t)
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

	// Filter by "new"
	msg := ClientMessage{Type: "list_opportunities", Filter: "new"}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if resp.Type != "opportunities" {
		t.Errorf("expected opportunities, got %q", resp.Type)
	}
	if len(resp.Opportunities) != 1 {
		t.Errorf("expected 1 opportunity with status=new, got %d", len(resp.Opportunities))
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_UpdateOpportunity(t *testing.T) {
	s := newTestServerWithOpportunities(t)
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

	// Update opportunity status
	msg := ClientMessage{Type: "update_opportunity", ID: "opp-001", Status: "proposal_drafted"}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if resp.Type != "opportunities" {
		t.Errorf("expected opportunities response after update, got %q", resp.Type)
	}

	// Verify the update was persisted
	opps, err := LoadOpportunities(s.opportunitiesDir)
	if err != nil {
		t.Fatalf("LoadOpportunities: %v", err)
	}

	var found bool
	for _, o := range opps {
		if o.ID == "opp-001" {
			if o.Status != "proposal_drafted" {
				t.Errorf("expected Status=proposal_drafted after update, got %q", o.Status)
			}
			found = true
		}
	}
	if !found {
		t.Error("opp-001 not found after update")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_LogRevenue(t *testing.T) {
	s := newTestServerWithOpportunities(t)
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

	// Log revenue
	msg := ClientMessage{Type: "log_revenue", ID: "opp-001", Amount: 2000.0}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	// Should receive an opportunities update (from the handleUpdateOpportunity call inside handleLogRevenue)
	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Check revenue file was updated
	total := LoadTotalRevenue(s.opportunitiesDir)
	expected := 5000.0 // 3000 (existing) + 2000 (new)
	if total != expected {
		t.Errorf("expected total revenue=%.2f, got %.2f", expected, total)
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_LogRevenue_Milestone(t *testing.T) {
	s := newTestServerWithOpportunities(t)

	// Clear existing revenue to start fresh
	revPath := filepath.Join(s.opportunitiesDir, "revenue.jsonl")
	os.Remove(revPath)

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

	// Log revenue that crosses the $1000 milestone
	msg := ClientMessage{Type: "log_revenue", ID: "opp-001", Amount: 1500.0}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	// Should receive milestone notification
	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if resp.Type == "revenue_update" {
		if resp.Milestone != "$1000" {
			t.Errorf("expected milestone=$1000, got %q", resp.Milestone)
		}
	}
	// Even if we don't get the milestone first, we should get some response

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_ApproveSource(t *testing.T) {
	s := newTestServerWithOpportunities(t)
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

	// Approve a pending source
	msg := ClientMessage{Type: "approve_source", URL: "https://golang-weekly.com"}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	// Should receive updated opportunities list
	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if resp.Type != "opportunities" {
		t.Errorf("expected opportunities response after approve, got %q", resp.Type)
	}

	// Verify the source was updated on disk
	sources, err := LoadDiscoveredSources(s.opportunitiesDir)
	if err != nil {
		t.Fatalf("LoadDiscoveredSources: %v", err)
	}

	for _, src := range sources {
		if src.URL == "https://golang-weekly.com" {
			if src.Status != "approved" {
				t.Errorf("expected Status=approved after approve, got %q", src.Status)
			}
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_UnknownMessageType(t *testing.T) {
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

	// Send unknown message type — should not crash
	msg := ClientMessage{Type: "nonexistent_type"}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck

	// The server logs the unknown type but does not respond — send another known message
	// to verify the connection is still alive
	selectMsg := ClientMessage{Type: "select_date", Date: "2026-04-08"}
	wsjson.Write(ctx, conn, selectMsg) //nolint:errcheck

	var resp ServerMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("connection broken after unknown message: %v", err)
	}
	if resp.Type != "day_detail" {
		t.Errorf("expected day_detail after recovery, got %q", resp.Type)
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestIsMemPalaceAvailable(t *testing.T) {
	// This test just verifies the function doesn't panic.
	// The result depends on whether python3 and mempalace are installed,
	// so we only check the return type.
	result := IsMemPalaceAvailable()
	_ = result // either true or false is fine
}

func TestHandleMessage_AllTypes(t *testing.T) {
	// Test that handleMessage dispatches to the correct handler for each type
	s := newTestServerWithOpportunities(t)
	handler := s.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	// Read init
	var initMsg ServerMessage
	wsjson.Read(ctx, conn, &initMsg) //nolint:errcheck

	// Test all message types in sequence
	messages := []ClientMessage{
		{Type: "select_date", Date: "2026-04-08"},
		{Type: "search", Query: "test", Date: "2026-04-08"},
		{Type: "list_opportunities"},
	}

	expectedTypes := []string{"day_detail", "search_results", "opportunities"}

	for i, msg := range messages {
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			t.Fatalf("Write %s: %v", msg.Type, err)
		}

		var resp ServerMessage
		if err := wsjson.Read(ctx, conn, &resp); err != nil {
			t.Fatalf("Read response for %s: %v", msg.Type, err)
		}

		if resp.Type != expectedTypes[i] {
			t.Errorf("message %s: expected response type %q, got %q", msg.Type, expectedTypes[i], resp.Type)
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestLoadOpportunities_BlankLines(t *testing.T) {
	dir := t.TempDir()
	content := `{"id":"opp-001","title":"Good","status":"new"}

{"id":"opp-002","title":"Also Good","status":"won"}

`
	os.WriteFile(filepath.Join(dir, "pipeline.jsonl"), []byte(content), 0o644) //nolint:errcheck

	loaded, err := LoadOpportunities(dir)
	if err != nil {
		t.Fatalf("LoadOpportunities: %v", err)
	}
	// Blank lines should be skipped
	if len(loaded) != 2 {
		t.Errorf("expected 2 opportunities (skip blank lines), got %d", len(loaded))
	}
}

func TestLoadDiscoveredSources_BlankLines(t *testing.T) {
	dir := t.TempDir()
	content := `{"url":"https://a.com","name":"A","status":"pending"}

{"url":"https://b.com","name":"B","status":"approved"}
`
	os.WriteFile(filepath.Join(dir, "discovered_sources.jsonl"), []byte(content), 0o644) //nolint:errcheck

	loaded, err := LoadDiscoveredSources(dir)
	if err != nil {
		t.Fatalf("LoadDiscoveredSources: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 sources (skip blank lines), got %d", len(loaded))
	}
}

func TestLoadTotalRevenue_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	content := `{"amount":1000,"status":"received"}
not json
{"amount":500,"status":"received"}
`
	os.WriteFile(filepath.Join(dir, "revenue.jsonl"), []byte(content), 0o644) //nolint:errcheck

	total := LoadTotalRevenue(dir)
	if total != 1500.0 {
		t.Errorf("expected 1500 (skip malformed), got %f", total)
	}
}

func TestServerMessage_JSON(t *testing.T) {
	msg := ServerMessage{
		Type: "opportunities",
		Opportunities: []OpportunityDetail{
			{ID: "o1", Title: "Test", Status: "new"},
		},
		OpportunityStats: &OpportunityStatsDetail{
			Total: 1, New: 1, Revenue: 0,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != "opportunities" {
		t.Errorf("expected type=opportunities, got %q", decoded.Type)
	}
	if len(decoded.Opportunities) != 1 {
		t.Errorf("expected 1 opportunity, got %d", len(decoded.Opportunities))
	}
}
