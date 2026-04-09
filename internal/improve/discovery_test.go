package improve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
)

func TestIsDiscoveryDay(t *testing.T) {
	tests := []struct {
		runCount int
		expected bool
	}{
		{1, false},
		{6, false},
		{7, true},
		{14, true},
		{15, false},
		{21, true},
	}
	for _, tt := range tests {
		result := improve.IsDiscoveryDay(tt.runCount)
		if result != tt.expected {
			t.Errorf("IsDiscoveryDay(%d) = %v, want %v", tt.runCount, result, tt.expected)
		}
	}
}

func TestDiscoverNewSources_ParsesSuggestions(t *testing.T) {
	// Mock Gemma 4 client
	client := &mockLLMClient{
		response: `{"sources": [
			{"url": "https://weworkremotely.com/remote-jobs", "name": "We Work Remotely", "reason": "Many high-budget backend jobs"},
			{"url": "https://toptal.com/developers", "name": "Toptal", "reason": "Pre-vetted freelancers, higher rates"},
			{"url": "https://gun.io", "name": "Gun.io", "reason": "Curated developer marketplace"}
		]}`,
	}

	// Mock Firecrawl for verification
	fcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"success": true,
			"data": map[string]any{
				"markdown": "# Jobs\n## Backend Developer\n**Company:** TestCo\nRemote position. Apply now. Salary: $100K-$150K",
				"metadata": map[string]any{"title": "Jobs Page"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer fcServer.Close()

	dir := t.TempDir()
	discoverer := improve.NewSourceDiscoverer(client, "fc-test-key", fcServer.URL, dir)

	sources, err := discoverer.DiscoverNewSources(context.Background(), []string{"Go", "REST API", "backend"})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sources) < 1 {
		t.Fatalf("expected at least 1 source, got %d", len(sources))
	}
	if sources[0].Status != "pending_approval" {
		t.Errorf("expected status pending_approval, got %q", sources[0].Status)
	}
}

func TestApproveSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovered_sources.jsonl")

	src := improve.DiscoveredSource{
		URL:    "https://weworkremotely.com",
		Name:   "We Work Remotely",
		Status: "pending_approval",
	}
	improve.AppendDiscoveredSource(path, src)

	err := improve.ApproveSource(path, "https://weworkremotely.com")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	sources, _ := improve.ReadDiscoveredSources(path)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Status != "approved" {
		t.Errorf("expected status approved, got %q", sources[0].Status)
	}
}

func TestApproveSource_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovered_sources.jsonl")
	os.WriteFile(path, []byte{}, 0o644)

	err := improve.ApproveSource(path, "https://nonexistent.com")
	if err == nil {
		t.Error("expected error for non-existent source")
	}
}
