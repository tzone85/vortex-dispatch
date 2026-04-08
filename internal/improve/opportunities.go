package improve

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status constants for the opportunity lifecycle.
const (
	StatusNew             = "new"
	StatusReviewed        = "reviewed"
	StatusInterested      = "interested"
	StatusProposalDrafted = "proposal_drafted"
	StatusSent            = "sent"
	StatusWon             = "won"
	StatusLost            = "lost"
	StatusExpired         = "expired"
)

// Opportunity represents a single job/bounty/contract opportunity.
type Opportunity struct {
	ID                string     `json:"id"`
	Source            string     `json:"source"`
	Title             string     `json:"title"`
	Company           string     `json:"company"`
	URL               string     `json:"url"`
	Budget            string     `json:"budget"`
	Skills            []string   `json:"skills"`
	Remote            bool       `json:"remote"`
	ScrapedAt         time.Time  `json:"scraped_at"`
	Status            string     `json:"status"`
	RelevanceScore    int        `json:"relevance_score"`
	BudgetScore       int        `json:"budget_score"`
	WinProbability    int        `json:"win_probability"`
	Rank              int        `json:"rank"`
	EffortEstimate    string     `json:"effort_estimate"`
	ProposalDraft     string     `json:"proposal_draft,omitempty"`
	ProposalDraftedAt *time.Time `json:"proposal_drafted_at,omitempty"`
	Revenue           float64    `json:"revenue"`
	Notes             string     `json:"notes"`
}

// RevenueEntry represents a single revenue event in the ledger.
type RevenueEntry struct {
	OpportunityID   string  `json:"opportunity_id"`
	Amount          float64 `json:"amount"`
	Currency        string  `json:"currency"`
	Date            string  `json:"date"`
	Status          string  `json:"status"`
	CumulativeTotal float64 `json:"cumulative_total"`
}

// DiscoveredSource represents an auto-discovered opportunity source.
type DiscoveredSource struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	DiscoveredOn string `json:"discovered_on"`
	Reason       string `json:"reason"`
	Status       string `json:"status"` // pending_approval, approved, rejected
}

// MissionMilestones are the revenue thresholds that trigger reminders.
var MissionMilestones = []float64{
	1000, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000,
}

// DefaultKeywordSets returns the 7-day keyword rotation sets.
func DefaultKeywordSets() [][]string {
	return [][]string{
		{"software developer", "backend"},
		{"web application", "fullstack"},
		{"API development", "REST", "GraphQL"},
		{"automation", "scripting", "CLI"},
		{"AI", "machine learning", "LLM integration"},
		{"Python", "Django", "FastAPI"},
		{"React", "Next.js", "Node.js", "TypeScript"},
	}
}

// KeywordsForDay returns the keyword set for a given day based on 7-day rotation.
func KeywordsForDay(sets [][]string, day time.Time) []string {
	if len(sets) == 0 {
		return nil
	}
	idx := day.YearDay() % len(sets)
	return sets[idx]
}

// GenerateOpportunityID creates a deterministic ID like opp-2026-04-09-001.
func GenerateOpportunityID(date string, seq int) string {
	return fmt.Sprintf("opp-%s-%03d", date, seq)
}

// ComputeRank calculates the combined rank: (relevance * 3) + (budget * 2) + win_probability.
func ComputeRank(opp Opportunity) int {
	return (opp.RelevanceScore * 3) + (opp.BudgetScore * 2) + opp.WinProbability
}

// AppendOpportunity writes a single opportunity to the JSONL pipeline file.
func AppendOpportunity(path string, opp Opportunity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create opportunities dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open pipeline: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(opp)
	if err != nil {
		return fmt.Errorf("marshal opportunity: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write opportunity: %w", err)
	}
	return f.Sync()
}

// ReadOpportunities reads all opportunities from the JSONL pipeline file.
func ReadOpportunities(path string) ([]Opportunity, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open pipeline: %w", err)
	}
	defer f.Close()

	var opps []Opportunity
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var opp Opportunity
		if err := json.Unmarshal([]byte(line), &opp); err != nil {
			continue // skip malformed lines
		}
		opps = append(opps, opp)
	}
	return opps, scanner.Err()
}

// UpdateOpportunityStatus reads the pipeline, updates the status of the matching
// opportunity, and rewrites the file. Returns the updated opportunity.
func UpdateOpportunityStatus(path, id, newStatus string) (Opportunity, error) {
	opps, err := ReadOpportunities(path)
	if err != nil {
		return Opportunity{}, fmt.Errorf("read pipeline: %w", err)
	}

	var updated Opportunity
	found := false
	newOpps := make([]Opportunity, 0, len(opps))
	for _, opp := range opps {
		if opp.ID == id {
			opp.Status = newStatus
			updated = opp
			found = true
		}
		newOpps = append(newOpps, opp)
	}
	if !found {
		return Opportunity{}, fmt.Errorf("opportunity %q not found", id)
	}

	if err := writeOpportunities(path, newOpps); err != nil {
		return Opportunity{}, fmt.Errorf("write pipeline: %w", err)
	}
	return updated, nil
}

// UpdateOpportunityField reads the pipeline, applies an update function to the
// matching opportunity, and rewrites the file. Returns the updated opportunity.
func UpdateOpportunityField(path, id string, updateFn func(Opportunity) Opportunity) (Opportunity, error) {
	opps, err := ReadOpportunities(path)
	if err != nil {
		return Opportunity{}, fmt.Errorf("read pipeline: %w", err)
	}

	var updated Opportunity
	found := false
	newOpps := make([]Opportunity, 0, len(opps))
	for _, opp := range opps {
		if opp.ID == id {
			opp = updateFn(opp)
			updated = opp
			found = true
		}
		newOpps = append(newOpps, opp)
	}
	if !found {
		return Opportunity{}, fmt.Errorf("opportunity %q not found", id)
	}

	if err := writeOpportunities(path, newOpps); err != nil {
		return Opportunity{}, fmt.Errorf("write pipeline: %w", err)
	}
	return updated, nil
}

// writeOpportunities overwrites the JSONL pipeline file with the given opportunities.
func writeOpportunities(path string, opps []Opportunity) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}
	defer f.Close()

	for _, opp := range opps {
		data, err := json.Marshal(opp)
		if err != nil {
			continue
		}
		f.Write(append(data, '\n'))
	}
	return f.Sync()
}

// FilterByStatus returns opportunities matching the given status.
func FilterByStatus(opps []Opportunity, status string) []Opportunity {
	result := make([]Opportunity, 0)
	for _, opp := range opps {
		if opp.Status == status {
			result = append(result, opp)
		}
	}
	return result
}

// SortByRank returns a new slice sorted by rank descending.
func SortByRank(opps []Opportunity) []Opportunity {
	sorted := make([]Opportunity, len(opps))
	copy(sorted, opps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Rank > sorted[j].Rank
	})
	return sorted
}

// TopN returns the top N opportunities by rank.
func TopN(opps []Opportunity, n int) []Opportunity {
	sorted := SortByRank(opps)
	if len(sorted) > n {
		return sorted[:n]
	}
	return sorted
}

// AppendRevenue writes a revenue entry to the JSONL ledger.
func AppendRevenue(path string, entry RevenueEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create revenue dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open revenue ledger: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal revenue entry: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write revenue entry: %w", err)
	}
	return f.Sync()
}

// ReadRevenue reads all entries from the revenue JSONL ledger.
func ReadRevenue(path string) ([]RevenueEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open revenue ledger: %w", err)
	}
	defer f.Close()

	var entries []RevenueEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e RevenueEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// TotalRevenue returns the sum of all received revenue entries.
func TotalRevenue(entries []RevenueEntry) float64 {
	total := 0.0
	for _, e := range entries {
		if e.Status == "received" {
			total += e.Amount
		}
	}
	return total
}

// CheckMilestone returns the highest milestone reached, or 0 if none.
func CheckMilestone(total float64) float64 {
	highest := 0.0
	for _, m := range MissionMilestones {
		if total >= m {
			highest = m
		}
	}
	return highest
}

// AppendDiscoveredSource writes a discovered source to the JSONL file.
func AppendDiscoveredSource(path string, src DiscoveredSource) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create sources dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open sources file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal source: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write source: %w", err)
	}
	return f.Sync()
}

// ReadDiscoveredSources reads all entries from the discovered sources JSONL.
func ReadDiscoveredSources(path string) ([]DiscoveredSource, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open sources file: %w", err)
	}
	defer f.Close()

	var sources []DiscoveredSource
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var s DiscoveredSource
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		sources = append(sources, s)
	}
	return sources, scanner.Err()
}
