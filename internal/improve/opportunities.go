package improve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

// OpportunityScraper fetches opportunities from free API sources.
type OpportunityScraper struct {
	jobicyBaseURL   string
	remotiveBaseURL string
	hnBaseURL       string
	firecrawlKey    string
	firecrawlURL    string
	client          *http.Client
}

// NewOpportunityScraper creates a scraper with configurable base URLs for testing.
func NewOpportunityScraper(jobicyBase, remotiveBase, hnBase string) *OpportunityScraper {
	if jobicyBase == "" {
		jobicyBase = "https://jobicy.com"
	}
	if remotiveBase == "" {
		remotiveBase = "https://remotive.com"
	}
	if hnBase == "" {
		hnBase = "https://hn.algolia.com"
	}
	return &OpportunityScraper{
		jobicyBaseURL:   jobicyBase,
		remotiveBaseURL: remotiveBase,
		hnBaseURL:       hnBase,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

// NewOpportunityScraperWithFirecrawl creates a scraper that also supports Firecrawl.
func NewOpportunityScraperWithFirecrawl(jobicyBase, remotiveBase, hnBase, firecrawlKey, firecrawlURL string) *OpportunityScraper {
	s := NewOpportunityScraper(jobicyBase, remotiveBase, hnBase)
	s.firecrawlKey = firecrawlKey
	s.firecrawlURL = firecrawlURL
	return s
}

// ScrapeJobicy fetches jobs from the Jobicy REST API.
func (s *OpportunityScraper) ScrapeJobicy(ctx context.Context, keywords []string) ([]Opportunity, error) {
	var allOpps []Opportunity

	for _, kw := range keywords {
		url := fmt.Sprintf("%s/api/v2/remote-jobs?count=50&industry=dev&tag=%s",
			s.jobicyBaseURL, strings.ReplaceAll(kw, " ", "+"))

		log.Printf("  [jobicy] Fetching keyword %q ...", kw)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log.Printf("  [jobicy] create request: %v", err)
			continue
		}

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("  [jobicy] http error for %q: %v", kw, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("jobicy returned %d for keyword %q", resp.StatusCode, kw)
		}

		var result struct {
			Jobs []struct {
				ID              int      `json:"id"`
				URL             string   `json:"url"`
				JobTitle        string   `json:"jobTitle"`
				CompanyName     string   `json:"companyName"`
				JobGeo          string   `json:"jobGeo"`
				AnnualSalaryMin string   `json:"annualSalaryMin"`
				AnnualSalaryMax string   `json:"annualSalaryMax"`
				JobIndustry     []string `json:"jobIndustry"`
			} `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			log.Printf("  [jobicy] decode error for %q: %v", kw, err)
			continue
		}
		resp.Body.Close()

		for _, j := range result.Jobs {
			budget := ""
			if j.AnnualSalaryMin != "" || j.AnnualSalaryMax != "" {
				budget = "$" + j.AnnualSalaryMin + "-$" + j.AnnualSalaryMax
			}
			opp := Opportunity{
				Source:  "jobicy",
				Title:   j.JobTitle,
				Company: j.CompanyName,
				URL:     j.URL,
				Budget:  budget,
				Skills:  j.JobIndustry,
				Remote:  true,
				Status:  StatusNew,
			}
			allOpps = append(allOpps, opp)
		}
		log.Printf("  [jobicy] Found %d jobs for %q", len(result.Jobs), kw)
	}

	return allOpps, nil
}

// ScrapeRemotive fetches jobs from the Remotive REST API.
func (s *OpportunityScraper) ScrapeRemotive(ctx context.Context) ([]Opportunity, error) {
	url := s.remotiveBaseURL + "/api/remote-jobs?category=software-dev"

	log.Printf("  [remotive] Fetching software-dev jobs ...")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remotive returned %d", resp.StatusCode)
	}

	var result struct {
		Jobs []struct {
			ID          int      `json:"id"`
			URL         string   `json:"url"`
			Title       string   `json:"title"`
			CompanyName string   `json:"company_name"`
			Tags        []string `json:"tags"`
			Salary      string   `json:"salary"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var opps []Opportunity
	for _, j := range result.Jobs {
		opp := Opportunity{
			Source:  "remotive",
			Title:   j.Title,
			Company: j.CompanyName,
			URL:     j.URL,
			Budget:  j.Salary,
			Skills:  j.Tags,
			Remote:  true,
			Status:  StatusNew,
		}
		opps = append(opps, opp)
	}

	log.Printf("  [remotive] Found %d jobs", len(opps))
	return opps, nil
}

// ScrapeHNWhoIsHiring fetches the current "Who is Hiring" thread from HN via Algolia.
func (s *OpportunityScraper) ScrapeHNWhoIsHiring(ctx context.Context, now time.Time) ([]Opportunity, error) {
	// Step 1: Find the thread ID for this month
	threadID, err := s.findHNThreadID(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("find HN thread: %w", err)
	}
	if threadID == "" {
		log.Printf("  [hn] No Who is Hiring thread found for %s", now.Format("January 2006"))
		return nil, nil
	}

	log.Printf("  [hn] Found thread ID %s, fetching comments ...", threadID)

	// Step 2: Fetch thread children (job postings)
	url := fmt.Sprintf("%s/api/v1/items/%s", s.hnBaseURL, threadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hn items returned %d", resp.StatusCode)
	}

	var thread struct {
		ID       int `json:"id"`
		Children []struct {
			ID     int    `json:"id"`
			Text   string `json:"text"`
			Author string `json:"author"`
		} `json:"children"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		return nil, fmt.Errorf("decode thread: %w", err)
	}

	var opps []Opportunity
	for _, child := range thread.Children {
		if child.Text == "" {
			continue
		}
		opp := parseHNComment(child.ID, child.Text, child.Author)
		opps = append(opps, opp)
	}

	log.Printf("  [hn] Parsed %d job postings from thread", len(opps))
	return opps, nil
}

// findHNThreadID searches Algolia for the current month's "Who is Hiring" thread.
func (s *OpportunityScraper) findHNThreadID(ctx context.Context, now time.Time) (string, error) {
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	timestamp := monthStart.Unix()

	url := fmt.Sprintf("%s/api/v1/search?query=%%22who+is+hiring%%22&tags=story&numericFilters=created_at_i>%d",
		s.hnBaseURL, timestamp)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create search request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	var searchResult struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return "", fmt.Errorf("decode search: %w", err)
	}

	for _, hit := range searchResult.Hits {
		lower := strings.ToLower(hit.Title)
		if strings.Contains(lower, "who is hiring") {
			return hit.ObjectID, nil
		}
	}

	// Fallback: try previous month
	prevMonth := monthStart.AddDate(0, -1, 0)
	prevTimestamp := prevMonth.Unix()
	url = fmt.Sprintf("%s/api/v1/search?query=%%22who+is+hiring%%22&tags=story&numericFilters=created_at_i>%d",
		s.hnBaseURL, prevTimestamp)

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil
	}
	resp2, err := s.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp2.Body.Close()

	var fallback struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	json.NewDecoder(resp2.Body).Decode(&fallback)

	for _, hit := range fallback.Hits {
		lower := strings.ToLower(hit.Title)
		if strings.Contains(lower, "who is hiring") {
			return hit.ObjectID, nil
		}
	}

	return "", nil
}

// parseHNComment extracts opportunity data from a HN job comment.
// HN comments typically follow: Company | Role | Location | Salary | ...
func parseHNComment(id int, text, author string) Opportunity {
	cleaned := SanitizeContent(text)
	parts := strings.SplitN(cleaned, "|", 4)

	company := strings.TrimSpace(parts[0])
	title := ""
	if len(parts) > 1 {
		title = strings.TrimSpace(parts[1])
	}
	budget := ""
	if len(parts) > 2 {
		for _, part := range parts[2:] {
			trimmed := strings.TrimSpace(part)
			if strings.Contains(trimmed, "$") || strings.Contains(strings.ToLower(trimmed), "k") {
				budget = trimmed
				break
			}
		}
	}

	return Opportunity{
		Source:  "hn_who_is_hiring",
		Title:   title,
		Company: company,
		URL:     fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id),
		Budget:  budget,
		Remote:  strings.Contains(strings.ToLower(cleaned), "remote"),
		Status:  StatusNew,
		Notes:   cleaned,
	}
}
