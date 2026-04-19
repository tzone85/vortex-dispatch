package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"net"
	"path/filepath"
	"sort"
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

	fmt.Fprintf(os.Stderr, "=== VXD Self-Improvement Engine — %s ===\n", date)

	// Wait for network — Mac may have just woken from sleep and WiFi isn't ready yet.
	// launchd fires immediately on wake, but DNS can take 5-15 seconds to resolve.
	waitForNetwork(30 * time.Second)

	// Idempotency check
	runsDir := filepath.Join(cfg.AuditDir, "runs")
	if improve.IsRunComplete(runsDir, date) {
		fmt.Fprintf(os.Stderr, "Run for %s already complete — skipping (delete %s/%s.json to re-run)\n", date, runsDir, date)
		os.Exit(0)
	}

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

		// Skip implementation for non-actionable findings (competitor intel, news)
		if !sf.Actionable {
			log.Printf("  [proposed] %s (intelligence only, not actionable)", sf.Title)
			auditLog.Append(improve.AuditEntry{
				RunID:       runID,
				FindingID:   fmt.Sprintf("f-%s-%03d", date, i+1),
				Source:      sf.SourceURL,
				Category:    sf.Category,
				Title:       sf.Title,
				Relevance:   sf.Relevance,
				Impact:      sf.Impact,
				Risk:        sf.Risk,
				Disposition: "proposed",
				Reasoning:   sf.Reasoning,
			})
			summary.PRsProposed++
			continue
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
		if result.Error != "" {
			summary.Errors = append(summary.Errors, fmt.Sprintf("[%s] %s: %s", result.Disposition, sf.Title, result.Error))
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
			Error:          result.Error,
			Reasoning:      sf.Reasoning,
			SecurityReview: af.SecurityReview,
			LicenseCheck:   af.LicenseCheck,
		})

		log.Printf("  [%s] %s", result.Disposition, sf.Title)
	}

	summary.PRsCreated = prsCreated

	// Phase 5: Audit (moved before phases 7/8; email sent after all data gathered)
	log.Println("Phase 5: Audit")
	summary.CompletedAt = time.Now()
	if err := improve.SaveRunSummary(runsDir, date, summary); err != nil {
		log.Printf("Save run summary error: %v", err)
	}

	// Phase 6: MemPalace re-mine (keep semantic memory current)
	log.Println("Phase 6: MemPalace re-mine")
	if mempalacePath, err := exec.LookPath("python3"); err == nil {
		mineCmd := exec.Command(mempalacePath, "-m", "mempalace", "mine", cfg.RepoPath)
		mineCmd.Dir = cfg.RepoPath
		if out, err := mineCmd.CombinedOutput(); err != nil {
			log.Printf("  MemPalace re-mine failed: %v\n%s", err, string(out))
		} else {
			log.Println("  MemPalace re-mine complete")
		}
	} else {
		log.Println("  MemPalace not installed (python3 not found), skipping")
	}

	// Phase 7: Opportunity Scanning
	log.Println("Phase 7: Opportunity Scanning")
	keywords := improve.KeywordsForDay(cfg.OpportunityKeywords, now)
	log.Printf("  Keywords for today: %v", keywords)

	oppScraper := improve.NewOpportunityScraperWithFirecrawl(
		"", "", "", // use default base URLs
		cfg.FirecrawlKey, "https://api.firecrawl.dev",
	)
	rawOpps, err := oppScraper.ScrapeAllSources(ctx, keywords, now)
	if err != nil {
		log.Printf("  Opportunity scraping error: %v", err)
	}
	log.Printf("  Scraped %d raw opportunities", len(rawOpps))

	// Score with Gemma 4
	var scoredOpps []improve.Opportunity
	if len(rawOpps) > 0 {
		scoredOpps, err = improve.ScoreOpportunities(ctx, rawOpps, googleClient)
		if err != nil {
			log.Printf("  Opportunity scoring error: %v", err)
		}
	}

	// Apply Bayesian feedback adjustments to scored opportunities
	feedbackPath := filepath.Join(cfg.OpportunitiesDir, "feedback.jsonl")
	feedback := improve.NewFeedbackLoop(feedbackPath)
	adjustments := feedback.ComputeAdjustments()
	if len(adjustments) > 0 {
		log.Printf("  Applying %d feedback adjustments to opportunity scores", len(adjustments))
		adjusted := make([]improve.Opportunity, len(scoredOpps))
		for i, opp := range scoredOpps {
			adjusted[i] = feedback.AdjustOpportunityScore(opp, adjustments)
		}
		scoredOpps = adjusted
	}

	// Write feedback insights for MemPalace
	insights := feedback.GenerateInsights()
	if len(insights) > 0 {
		insightsPath := filepath.Join(cfg.AuditDir, "feedback_insights.md")
		if err := os.WriteFile(insightsPath, []byte(insights), 0o644); err != nil {
			log.Printf("  Write feedback insights error: %v", err)
		} else {
			log.Println("  Wrote feedback insights for MemPalace")
		}
	}

	// Filter and assign IDs
	filteredOpps := improve.FilterAndRankOpportunities(scoredOpps, 5)
	pipelinePath := filepath.Join(cfg.OpportunitiesDir, "pipeline.jsonl")
	for i, opp := range filteredOpps {
		opp.ID = improve.GenerateOpportunityID(date, i+1)
		opp.ScrapedAt = now
		if err := improve.AppendOpportunity(pipelinePath, opp); err != nil {
			log.Printf("  Failed to save opportunity %s: %v", opp.ID, err)
		}
	}
	log.Printf("  Saved %d scored opportunities to pipeline", len(filteredOpps))

	// Draft proposals if active bidding is enabled
	var proposalResults []improve.Opportunity
	if cfg.ActiveBidding && len(filteredOpps) > 0 {
		log.Println("  Active bidding enabled — drafting proposals for top opportunities")
		proposalDir := filepath.Join(cfg.OpportunitiesDir, "proposals")
		drafter := improve.NewProposalDrafter(cfg.ClaudePath, proposalDir)
		proposalResults = drafter.DraftProposalsForTop(ctx, filteredOpps, cfg.MaxProposalsPerDay)
		log.Printf("  Drafted %d proposals", len(proposalResults))

		// Update pipeline with proposal data
		for _, opp := range proposalResults {
			improve.UpdateOpportunityField(pipelinePath, opp.ID, func(existing improve.Opportunity) improve.Opportunity {
				existing.ProposalDraft = opp.ProposalDraft
				existing.ProposalDraftedAt = opp.ProposalDraftedAt
				existing.Status = improve.StatusProposalDrafted
				return existing
			})
		}
	} else if !cfg.ActiveBidding {
		log.Println("  Observation mode — no proposals drafted (set VXD_ACTIVE_BIDDING=true to enable)")
	}

	// Revenue summary
	revenuePath := filepath.Join(cfg.OpportunitiesDir, "revenue.jsonl")
	revenueEntries, _ := improve.ReadRevenue(revenuePath)
	totalRevenue := improve.TotalRevenue(revenueEntries)
	milestone := improve.CheckMilestone(totalRevenue)

	// Phase 8: Weekly Source Discovery
	log.Println("Phase 8: Source Discovery")
	allPipelineOpps, _ := improve.ReadOpportunities(pipelinePath)
	runCount := len(allPipelineOpps) / 10 // Approximate run count from pipeline size
	if improve.IsDiscoveryDay(runCount) {
		log.Println("  Discovery day — analyzing week's data for new sources")
		topSkills := extractTopSkills(filteredOpps)
		discoverer := improve.NewSourceDiscoverer(googleClient, cfg.FirecrawlKey, "https://api.firecrawl.dev", cfg.OpportunitiesDir)
		newSources, err := discoverer.DiscoverNewSources(ctx, topSkills)
		if err != nil {
			log.Printf("  Source discovery error: %v", err)
		} else {
			log.Printf("  Discovered %d new sources (pending approval)", len(newSources))
		}
	} else {
		log.Println("  Not a discovery day, skipping")
	}

	// Phase 9: Email (after all data gathered)
	log.Println("Phase 9: Email")
	emailData := buildEmailData(date, findings, results, summary, filteredOpps, proposalResults, allPipelineOpps, totalRevenue, milestone)

	html, err := improve.BuildEmailHTML(emailData)
	if err != nil {
		log.Printf("Build email error: %v", err)
		summary.Errors = append(summary.Errors, fmt.Sprintf("email build: %v", err))
	} else {
		sender := improve.NewEmailSender(cfg.ResendKey, "https://api.resend.com")
		subject := fmt.Sprintf("VXD Daily Improvement Report — %s (%d PRs, %d Alerts, %d Opps)",
			date, prsCreated, len(emailData.SecurityAlerts), len(filteredOpps))

		if err := sender.Send(ctx, subject, html, cfg.EmailTo, cfg.EmailFrom); err != nil {
			log.Printf("Send email error: %v", err)
			summary.Errors = append(summary.Errors, fmt.Sprintf("email send: %v", err))
		} else {
			summary.EmailSent = true
			log.Println("  Email sent successfully")
		}

		// Weekly digest — sent on Sundays alongside the daily report
		if improve.IsWeeklyDigestDay(now) {
			log.Println("  Sunday — generating weekly digest")
			digest := improve.BuildWeeklyDigest(cfg.AuditDir, cfg.OpportunitiesDir, now)
			weeklyHTML, err := improve.BuildWeeklyEmailHTML(digest)
			if err != nil {
				log.Printf("  Weekly digest build error: %v", err)
			} else {
				weeklySubject := fmt.Sprintf("VXD Weekly Digest — Week %d (%d PRs, %d Opps, $%.0f revenue)",
					digest.WeekNumber, digest.PRsCreated, digest.NewOpportunities, digest.RevenueCumulative)
				if err := sender.Send(ctx, weeklySubject, weeklyHTML, cfg.EmailTo, cfg.EmailFrom); err != nil {
					log.Printf("  Weekly digest send error: %v", err)
				} else {
					log.Println("  Weekly digest sent successfully")
				}
			}
		}
	}

	log.Printf("=== Complete: %d findings, %d PRs, %d opportunities, email=%v ===",
		summary.FindingsTotal, summary.PRsCreated, len(filteredOpps), summary.EmailSent)
}

func buildEmailData(date string, findings []improve.Finding, results []improve.ImplementResult, summary improve.RunSummary, todayOpps []improve.Opportunity, proposals []improve.Opportunity, allOpps []improve.Opportunity, totalRevenue float64, milestone float64) improve.EmailData {
	data := improve.EmailData{
		Date:       date,
		PRsCreated: summary.PRsCreated,
		Summary: fmt.Sprintf("Scraped %d sources, found %d findings, %d relevant, implemented %d. Opportunities: %d new.",
			summary.SourcesScraped, summary.FindingsTotal, summary.FindingsRelevant, summary.PRsCreated, len(todayOpps)),
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

	// Add opportunity data
	topOpps := improve.TopN(todayOpps, 10)
	for _, opp := range topOpps {
		hasDraft := opp.ProposalDraft != ""
		data.Opportunities = append(data.Opportunities, improve.EmailOpportunity{
			Title:    opp.Title,
			URL:      opp.URL,
			Source:   opp.Source,
			Budget:   opp.Budget,
			Rank:     opp.Rank,
			Status:   opp.Status,
			HasDraft: hasDraft,
		})
	}
	if len(data.Opportunities) > 0 {
		data.OpportunityStats = &improve.OpportunityStats{
			TotalPipeline:    len(allOpps),
			NewToday:         len(todayOpps),
			ProposalsDrafted: len(proposals),
			TotalRevenue:     totalRevenue,
		}
	}

	// Mission milestone
	if milestone > 0 {
		data.MissionMilestone = &improve.MissionMilestoneData{
			Amount:  milestone,
			Message: "You started this to free your village from poverty.",
		}
	}

	data.AlertCount = len(data.SecurityAlerts)
	return data
}

func extractTopSkills(opps []improve.Opportunity) []string {
	skillCounts := make(map[string]int)
	for _, opp := range opps {
		for _, skill := range opp.Skills {
			skillCounts[skill]++
		}
	}
	type kv struct {
		Key   string
		Count int
	}
	var sorted []kv
	for k, v := range skillCounts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })
	var result []string
	for i, s := range sorted {
		if i >= 5 {
			break
		}
		result = append(result, s.Key)
	}
	if len(result) == 0 {
		result = []string{"software development", "backend", "API"}
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// waitForNetwork polls DNS resolution until it succeeds or timeout expires.
// This handles the case where launchd fires immediately on Mac wake but
// WiFi hasn't reconnected yet (DNS fails with "no such host").
func waitForNetwork(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	testHost := "api.resend.com"

	for time.Now().Before(deadline) {
		_, err := net.LookupHost(testHost)
		if err == nil {
			return // network is ready
		}
		log.Printf("Waiting for network (DNS lookup failed: %v)...", err)
		time.Sleep(3 * time.Second)
	}
	log.Printf("WARNING: Network not available after %s — proceeding anyway (may fail)", timeout)
}
