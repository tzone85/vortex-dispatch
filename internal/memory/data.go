// internal/memory/data.go
package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TimelineEntry represents a single day on the timeline.
type TimelineEntry struct {
	Date          string `json:"date"`
	PRs           int    `json:"prs"`
	Findings      int    `json:"findings"`
	Commits       int    `json:"commits"`
	ActivityLevel int    `json:"activity_level"`
}

// Timeline holds the full set of entries and the date range.
type Timeline struct {
	Entries []TimelineEntry `json:"entries"`
	Min     string          `json:"min"`
	Max     string          `json:"max"`
	Today   string          `json:"today"`
}

// FindingDetail represents one finding from changelog.jsonl.
type FindingDetail struct {
	FindingID      string `json:"finding_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	Date           string `json:"date,omitempty"`
	Title          string `json:"title"`
	Relevance      int    `json:"relevance"`
	Impact         int    `json:"impact"`
	Risk           int    `json:"risk"`
	Rank           int    `json:"rank"`
	Disposition    string `json:"disposition"`
	Category       string `json:"category"`
	SourceURL      string `json:"source_url"`
	Reasoning      string `json:"reasoning"`
	PRURL          string `json:"pr_url,omitempty"`
	TestsPassed    bool   `json:"tests_passed"`
	SecurityReview string `json:"security_review,omitempty"`
	LicenseCheck   string `json:"license_check,omitempty"`
}

// PRDetail represents a pull request linked to a finding.
type PRDetail struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Status   string `json:"status"`
	Category string `json:"category"`
	Lines    int    `json:"lines"`
}

// CommitDetail represents a git commit.
type CommitDetail struct {
	SHA       string `json:"sha"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// RunSummaryDetail holds the summary for a single self-improvement run.
type RunSummaryDetail struct {
	SourcesScraped   int  `json:"sources_scraped"`
	FindingsTotal    int  `json:"findings_total"`
	FindingsRelevant int  `json:"findings_relevant"`
	PRsCreated       int  `json:"prs_created"`
	EmailSent        bool `json:"email_sent"`
}

// DayDetail holds all data for a selected date.
type DayDetail struct {
	Date       string            `json:"date"`
	PRs        []PRDetail        `json:"prs"`
	Findings   []FindingDetail   `json:"findings"`
	Commits    []CommitDetail    `json:"commits"`
	RunSummary *RunSummaryDetail `json:"run_summary"`
}

// changelogEntry is the raw JSONL record from changelog.jsonl.
type changelogEntry struct {
	RunID          string `json:"run_id"`
	FindingID      string `json:"finding_id"`
	Source         string `json:"source"`
	Category       string `json:"category"`
	Title          string `json:"title"`
	Relevance      int    `json:"relevance"`
	Impact         int    `json:"impact"`
	Risk           int    `json:"risk"`
	Disposition    string `json:"disposition"`
	Reasoning      string `json:"reasoning"`
	PRURL          string `json:"pr_url"`
	Lines          int    `json:"lines_changed"`
	TestsPassed    bool   `json:"tests_passed"`
	SecurityReview string `json:"security_review"`
	LicenseCheck   string `json:"license_check"`
}

// runSummary is the raw JSON from runs/YYYY-MM-DD.json.
type runSummary struct {
	RunID            string `json:"run_id"`
	SourcesScraped   int    `json:"sources_scraped"`
	FindingsTotal    int    `json:"findings_total"`
	FindingsRelevant int    `json:"findings_relevant"`
	PRsCreated       int    `json:"prs_created"`
	EmailSent        bool   `json:"email_sent"`
}

// BuildTimeline reads changelog.jsonl and runs/*.json to build a timeline.
func BuildTimeline(auditDir string) (Timeline, error) {
	today := time.Now().Format("2006-01-02")
	tl := Timeline{Today: today}

	// Aggregate per-date counts from changelog
	dateFindings := make(map[string]int)
	datePRs := make(map[string]int)

	changelogPath := filepath.Join(auditDir, "changelog.jsonl")
	if entries, err := readChangelog(changelogPath); err == nil {
		for _, e := range entries {
			date := extractDate(e.RunID)
			if date == "" {
				continue
			}
			dateFindings[date]++
			if e.PRURL != "" {
				datePRs[date]++
			}
		}
	}

	// Read run summaries for PR counts (prefer run summary over changelog)
	runsDir := filepath.Join(auditDir, "runs")
	runFiles, _ := filepath.Glob(filepath.Join(runsDir, "*.json"))
	for _, rf := range runFiles {
		base := strings.TrimSuffix(filepath.Base(rf), ".json")
		data, err := os.ReadFile(rf)
		if err != nil {
			continue
		}
		var rs runSummary
		if err := json.Unmarshal(data, &rs); err != nil {
			continue
		}
		if rs.PRsCreated > 0 {
			datePRs[base] = rs.PRsCreated
		}
		// Ensure date is in the map even if no findings
		if _, ok := dateFindings[base]; !ok {
			dateFindings[base] = rs.FindingsRelevant
		}
	}

	// Collect all dates
	allDates := make(map[string]bool)
	for d := range dateFindings {
		allDates[d] = true
	}
	for d := range datePRs {
		allDates[d] = true
	}

	if len(allDates) == 0 {
		tl.Min = today
		tl.Max = today
		return tl, nil
	}

	sorted := make([]string, 0, len(allDates))
	for d := range allDates {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)

	tl.Min = sorted[0]
	tl.Max = sorted[len(sorted)-1]

	for _, d := range sorted {
		findings := dateFindings[d]
		prs := datePRs[d]
		entry := TimelineEntry{
			Date:          d,
			PRs:           prs,
			Findings:      findings,
			ActivityLevel: ActivityLevel(prs, findings, 0),
		}
		tl.Entries = append(tl.Entries, entry)
	}

	return tl, nil
}

// GetDayDetail returns findings, PRs, commits, and run summary for a date.
func GetDayDetail(auditDir, date string) (DayDetail, error) {
	dd := DayDetail{Date: date}

	// Parse findings from changelog
	changelogPath := filepath.Join(auditDir, "changelog.jsonl")
	entries, err := readChangelog(changelogPath)
	if err != nil && !os.IsNotExist(err) {
		return dd, fmt.Errorf("read changelog: %w", err)
	}

	for _, e := range entries {
		entryDate := extractDate(e.RunID)
		if entryDate != date {
			continue
		}
		finding := findingDetailFromEntry(e)
		dd.Findings = append(dd.Findings, finding)

		if e.PRURL != "" {
			pr := PRDetail{
				Title:    e.Title,
				URL:      e.PRURL,
				Status:   e.Disposition,
				Category: e.Category,
				Lines:    e.Lines,
			}
			dd.PRs = append(dd.PRs, pr)
		}
	}

	// Read run summary
	runPath := filepath.Join(auditDir, "runs", date+".json")
	data, err := os.ReadFile(runPath)
	if err == nil {
		var rs runSummary
		if json.Unmarshal(data, &rs) == nil {
			dd.RunSummary = &RunSummaryDetail{
				SourcesScraped:   rs.SourcesScraped,
				FindingsTotal:    rs.FindingsTotal,
				FindingsRelevant: rs.FindingsRelevant,
				PRsCreated:       rs.PRsCreated,
				EmailSent:        rs.EmailSent,
			}
		}
	}

	return dd, nil
}

// ListFindings returns the full self-improvement audit log as dashboard-friendly
// finding cards sorted newest-first, then highest rank.
func ListFindings(auditDir string) ([]FindingDetail, error) {
	changelogPath := filepath.Join(auditDir, "changelog.jsonl")
	entries, err := readChangelog(changelogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read changelog: %w", err)
	}

	findings := make([]FindingDetail, 0, len(entries))
	for _, entry := range entries {
		findings = append(findings, findingDetailFromEntry(entry))
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RunID == findings[j].RunID {
			if findings[i].Rank == findings[j].Rank {
				return findings[i].Title < findings[j].Title
			}
			return findings[i].Rank > findings[j].Rank
		}
		return findings[i].RunID > findings[j].RunID
	})

	return findings, nil
}

// GetCommitsForDate returns git commits for a specific date.
func GetCommitsForDate(repoDir, date string) []CommitDetail {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	after := parsed.Format("2006-01-02")
	before := parsed.AddDate(0, 0, 1).Format("2006-01-02")

	cmd := exec.Command("git", "log",
		"--format=%H|%s|%aI",
		"--after="+after,
		"--before="+before,
	)
	cmd.Dir = repoDir

	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var commits []CommitDetail
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, CommitDetail{
			SHA:       parts[0],
			Message:   parts[1],
			Timestamp: parts[2],
		})
	}
	return commits
}

// ActivityLevel maps counts to a 0-3 heatmap level.
func ActivityLevel(prs, findings, commits int) int {
	total := prs*3 + findings + commits
	switch {
	case total == 0:
		return 0
	case total <= 3:
		return 1
	case total <= 8:
		return 2
	default:
		return 3
	}
}

// readChangelog reads all entries from a changelog.jsonl file.
func readChangelog(path string) ([]changelogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []changelogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e changelogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			log.Printf("[memory] malformed changelog line in %s: %v", path, err)
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// OpportunityDetail represents an opportunity for the dashboard.
type OpportunityDetail struct {
	ID             string   `json:"id"`
	Source         string   `json:"source"`
	Title          string   `json:"title"`
	Company        string   `json:"company"`
	URL            string   `json:"url"`
	Budget         string   `json:"budget"`
	Skills         []string `json:"skills"`
	Status         string   `json:"status"`
	RelevanceScore int      `json:"relevance_score"`
	BudgetScore    int      `json:"budget_score"`
	WinProbability int      `json:"win_probability"`
	Rank           int      `json:"rank"`
	EffortEstimate string   `json:"effort_estimate"`
	ProposalDraft  string   `json:"proposal_draft,omitempty"`
	Revenue        float64  `json:"revenue"`
}

// OpportunityStatsDetail holds pipeline stats for the dashboard.
type OpportunityStatsDetail struct {
	Total   int     `json:"total"`
	New     int     `json:"new"`
	Drafts  int     `json:"drafts"`
	Won     int     `json:"won"`
	Revenue float64 `json:"revenue"`
}

// DiscoveredSourceDetail represents a discovered source for the dashboard.
type DiscoveredSourceDetail struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	DiscoveredOn string `json:"discovered_on"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
}

// LoadOpportunities reads the pipeline JSONL and returns opportunity details.
func LoadOpportunities(opportunitiesDir string) ([]OpportunityDetail, error) {
	path := filepath.Join(opportunitiesDir, "pipeline.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var opps []OpportunityDetail
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var opp OpportunityDetail
		if err := json.Unmarshal([]byte(line), &opp); err != nil {
			continue
		}
		opps = append(opps, opp)
	}
	return opps, scanner.Err()
}

// ComputeOpportunityStats computes aggregate stats from the pipeline.
func ComputeOpportunityStats(opps []OpportunityDetail, revenue float64) OpportunityStatsDetail {
	stats := OpportunityStatsDetail{Total: len(opps), Revenue: revenue}
	for _, o := range opps {
		switch o.Status {
		case "new":
			stats.New++
		case "proposal_drafted":
			stats.Drafts++
		case "won":
			stats.Won++
		}
	}
	return stats
}

// LoadDiscoveredSources reads the discovered sources JSONL.
func LoadDiscoveredSources(opportunitiesDir string) ([]DiscoveredSourceDetail, error) {
	path := filepath.Join(opportunitiesDir, "discovered_sources.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var sources []DiscoveredSourceDetail
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var s DiscoveredSourceDetail
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		sources = append(sources, s)
	}
	return sources, scanner.Err()
}

// LoadTotalRevenue reads the revenue ledger and returns the total.
func LoadTotalRevenue(opportunitiesDir string) float64 {
	path := filepath.Join(opportunitiesDir, "revenue.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	total := 0.0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry struct {
			Amount float64 `json:"amount"`
			Status string  `json:"status"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Status == "received" {
			total += entry.Amount
		}
	}
	return total
}

// extractDate pulls the YYYY-MM-DD portion from a run_id timestamp.
func extractDate(runID string) string {
	if len(runID) < 10 {
		return ""
	}
	datePart := runID[:10]
	_, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return ""
	}
	return datePart
}

func findingDetailFromEntry(entry changelogEntry) FindingDetail {
	return FindingDetail{
		FindingID:      entry.FindingID,
		RunID:          entry.RunID,
		Date:           extractDate(entry.RunID),
		Title:          entry.Title,
		Relevance:      entry.Relevance,
		Impact:         entry.Impact,
		Risk:           entry.Risk,
		Rank:           findingRank(entry.Relevance, entry.Impact, entry.Risk),
		Disposition:    entry.Disposition,
		Category:       entry.Category,
		SourceURL:      entry.Source,
		Reasoning:      entry.Reasoning,
		PRURL:          entry.PRURL,
		TestsPassed:    entry.TestsPassed,
		SecurityReview: entry.SecurityReview,
		LicenseCheck:   entry.LicenseCheck,
	}
}

func findingRank(relevance, impact, risk int) int {
	return (impact * 2) + relevance - risk
}
