package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// countValidJSONLines returns the number of non-empty lines in a JSONL file
// and the number of those that fail to parse as a JSON object. A truncate-and-
// rewrite that interleaves with another writer leaves partial/overlapping
// lines, which show up here as a wrong record count or a parse failure.
func countValidJSONLines(t *testing.T, path string) (records, corrupt int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		records++
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			corrupt++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return records, corrupt
}

// TestConcurrentOpportunityWrites_NoCorruption hammers the opportunity-file
// read-modify-write handlers from many goroutines at once, as two dashboard
// tabs (each its own WebSocket connection / net.http goroutine) would. The
// serializing oppMu must keep pipeline.jsonl and discovered_sources.jsonl
// intact: no records dropped, no partial/garbled lines. Before the lock, the
// concurrent os.Create truncate+rewrite interleaved and silently lost records.
func TestConcurrentOpportunityWrites_NoCorruption(t *testing.T) {
	s := newTestServerWithOpportunities(t)
	oppDir := s.opportunitiesDir
	pipelinePath := filepath.Join(oppDir, "pipeline.jsonl")
	sourcesPath := filepath.Join(oppDir, "discovered_sources.jsonl")

	const (
		workers = 48
		rounds  = 25
	)
	oppIDs := []string{"opp-001", "opp-002", "opp-003"}
	srcURLs := []string{"https://golang-weekly.com", "https://remote.co"}
	statuses := []string{"new", "proposal_drafted", "won"}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				id := oppIDs[(w+r)%len(oppIDs)]
				status := statuses[(w+r)%len(statuses)]
				if err := s.updateOpportunityStatus(id, status); err != nil {
					t.Errorf("updateOpportunityStatus: %v", err)
					return
				}
				url := srcURLs[(w+r)%len(srcURLs)]
				if err := s.approveSource(url); err != nil {
					t.Errorf("approveSource: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// Both files must still hold exactly their original record count with no
	// corrupt lines — updates only mutate a field, never add or drop records.
	if rec, bad := countValidJSONLines(t, pipelinePath); rec != len(oppIDs) || bad != 0 {
		t.Errorf("pipeline.jsonl corrupted: got %d records (%d unparseable), want %d valid", rec, bad, len(oppIDs))
	}
	if rec, bad := countValidJSONLines(t, sourcesPath); rec != len(srcURLs) || bad != 0 {
		t.Errorf("discovered_sources.jsonl corrupted: got %d records (%d unparseable), want %d valid", rec, bad, len(srcURLs))
	}

	// LoadOpportunities (which silently skips unparseable lines) must still
	// see every original opportunity: no record was lost to an interleaved write.
	opps, err := LoadOpportunities(oppDir)
	if err != nil {
		t.Fatalf("LoadOpportunities after concurrent writes: %v", err)
	}
	seen := map[string]bool{}
	for _, o := range opps {
		seen[o.ID] = true
	}
	for _, id := range oppIDs {
		if !seen[id] {
			t.Errorf("opportunity %s lost after concurrent writes", id)
		}
	}
}

// TestConcurrentLogRevenue_NoLostUpdate records revenue from many goroutines
// concurrently. Each entry is appended under oppMu, so the ledger total must
// equal the initial balance plus every logged amount — none dropped, none
// double-counted.
func TestConcurrentLogRevenue_NoLostUpdate(t *testing.T) {
	s := newTestServerWithOpportunities(t)
	oppDir := s.opportunitiesDir

	start := LoadTotalRevenue(oppDir) // 3000 from the fixture
	const (
		workers = 32
		amount  = 100.0
	)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.logRevenue("opp-001", amount); err != nil {
				t.Errorf("logRevenue: %v", err)
			}
		}()
	}
	wg.Wait()

	got := LoadTotalRevenue(oppDir)
	want := start + workers*amount
	if got != want {
		t.Errorf("revenue total = %.2f, want %.2f (lost/garbled revenue entries)", got, want)
	}
}
