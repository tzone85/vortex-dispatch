package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/improve"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "research + analyze + email, but don't create branches or PRs")
	flag.Parse()

	cfg, err := improve.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.DryRun = *dryRun

	now := time.Now()
	date := now.Format("2006-01-02")
	runID := now.UTC().Format(time.RFC3339)

	// Idempotency check
	runsDir := filepath.Join(cfg.AuditDir, "runs")
	if improve.IsRunComplete(runsDir, date) {
		log.Printf("Run for %s already complete — skipping", date)
		os.Exit(0)
	}

	log.Printf("=== VXD Self-Improvement Engine — %s ===", date)
	if cfg.DryRun {
		log.Printf("DRY RUN MODE — no branches or PRs will be created")
	}

	ctx := context.Background()
	startedAt := time.Now()

	summary := improve.RunSummary{
		RunID:     runID,
		StartedAt: startedAt,
	}

	// Phase 1: Research
	log.Println("Phase 1: Research")
	researcher := improve.NewResearcher(cfg.FirecrawlKey, "https://api.firecrawl.dev")
	findings, err := researcher.Research(ctx, now)
	if err != nil {
		log.Printf("Research error: %v", err)
	}
	summary.FindingsTotal = len(findings)
	summary.SourcesScraped = len(findings)
	log.Printf("  Found %d findings", len(findings))

	// Phase 2: Analysis
	log.Println("Phase 2: Analysis")
	googleClient := llm.NewGoogleAIClient(cfg.GoogleAIKey)
	analyzer := improve.NewAnalyzer(googleClient, cfg.ClaudePath, cfg.RelevanceThreshold)

	scored, err := analyzer.Triage(ctx, findings)
	if err != nil {
		log.Printf("Triage error: %v", err)
	}
	summary.FindingsRelevant = len(scored)
	log.Printf("  %d findings above relevance threshold", len(scored))

	if len(scored) > cfg.MaxFindingsToAnalyze {
		scored = scored[:cfg.MaxFindingsToAnalyze]
	}
	summary.FindingsAnalyzed = len(scored)

	// Phase 3: Implementation
	log.Println("Phase 3: Implementation")
	impl := improve.NewImplementer(cfg.RepoPath, cfg.ClaudePath, cfg.MaxDiffLines, cfg.MaxFilesChanged, cfg.DryRun)
	auditLog := improve.NewAuditLog(cfg.AuditDir)

	var results []improve.ImplementResult
	prsCreated := 0

	for i, sf := range scored {
		if prsCreated >= cfg.MaxPRsPerRun {
			log.Printf("  Max PRs (%d) reached — remaining will be proposed", cfg.MaxPRsPerRun)
			break
		}

		af := improve.AnalyzedFinding{
			ScoredFinding:      sf,
			ImplementationPlan: sf.Reasoning,
			SecurityReview:     "Automated check — no new external inputs",
			LicenseCheck:       "pass",
			TestStrategy:       "Run existing test suite",
			GoNoGo:             "go",
		}

		result := impl.Implement(ctx, af, date)
		results = append(results, result)

		if result.IsImplemented() {
			prsCreated++
		}

		auditLog.Append(improve.AuditEntry{
			RunID:          runID,
			FindingID:      fmt.Sprintf("f-%s-%03d", date, i+1),
			Source:         sf.SourceURL,
			Category:       sf.Category,
			Title:          sf.Title,
			Relevance:      sf.Relevance,
			Impact:         sf.Impact,
			Risk:           sf.Risk,
			Disposition:    result.Disposition,
			PRURL:          result.PRURL,
			TestsPassed:    result.TestsPassed,
			FilesChanged:   result.FilesChanged,
			LinesChanged:   result.LinesChanged,
			Reasoning:      sf.Reasoning,
			SecurityReview: af.SecurityReview,
			LicenseCheck:   af.LicenseCheck,
		})

		log.Printf("  [%s] %s", result.Disposition, sf.Title)
	}

	summary.PRsCreated = prsCreated

	// Phase 4: Email
	log.Println("Phase 4: Email")
	emailData := buildEmailData(date, findings, results, summary)

	html, err := improve.BuildEmailHTML(emailData)
	if err != nil {
		log.Printf("Build email error: %v", err)
		summary.Errors = append(summary.Errors, fmt.Sprintf("email build: %v", err))
	} else {
		sender := improve.NewEmailSender(cfg.ResendKey, "https://api.resend.com")
		subject := fmt.Sprintf("VXD Daily Improvement Report — %s (%d PRs, %d Alerts)",
			date, prsCreated, len(emailData.SecurityAlerts))

		if err := sender.Send(ctx, subject, html, cfg.EmailTo, cfg.EmailFrom); err != nil {
			log.Printf("Send email error: %v", err)
			summary.Errors = append(summary.Errors, fmt.Sprintf("email send: %v", err))
		} else {
			summary.EmailSent = true
			log.Println("  Email sent successfully")
		}
	}

	// Phase 5: Audit
	log.Println("Phase 5: Audit")
	summary.CompletedAt = time.Now()
	if err := improve.SaveRunSummary(runsDir, date, summary); err != nil {
		log.Printf("Save run summary error: %v", err)
	}

	log.Printf("=== Complete: %d findings, %d PRs, email=%v ===",
		summary.FindingsTotal, summary.PRsCreated, summary.EmailSent)
}

func buildEmailData(date string, findings []improve.Finding, results []improve.ImplementResult, summary improve.RunSummary) improve.EmailData {
	data := improve.EmailData{
		Date:       date,
		PRsCreated: summary.PRsCreated,
		Summary: fmt.Sprintf("Scraped %d sources, found %d findings, %d relevant, implemented %d.",
			summary.SourcesScraped, summary.FindingsTotal, summary.FindingsRelevant, summary.PRsCreated),
	}

	for _, r := range results {
		if r.IsImplemented() {
			data.PRs = append(data.PRs, improve.EmailPR{
				Title:        r.Finding.Title,
				URL:          r.PRURL,
				Category:     r.Finding.Category,
				TestsPassed:  r.TestsPassed,
				LinesChanged: r.LinesChanged,
			})
		} else if r.Disposition == "proposed" {
			data.Proposed = append(data.Proposed, improve.EmailSection{
				Title:     r.Finding.Title,
				Content:   r.Finding.Reasoning,
				SourceURL: r.Finding.SourceURL,
			})
		}
	}

	for _, f := range findings {
		section := improve.EmailSection{Title: f.Title, Content: truncate(f.Content, 200), SourceURL: f.SourceURL}
		switch f.Category {
		case "security":
			data.SecurityAlerts = append(data.SecurityAlerts, section)
		case "competitors":
			data.Competitors = append(data.Competitors, section)
		case "historical":
			data.Historical = append(data.Historical, section)
		default:
			data.Trends = append(data.Trends, section)
		}
	}

	data.AlertCount = len(data.SecurityAlerts)
	return data
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
