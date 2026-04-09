package improve

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WeeklyDigest consolidates a week's worth of data into an actionable summary.
type WeeklyDigest struct {
	WeekEnding   string    `json:"week_ending"`
	WeekNumber   int       `json:"week_number"`
	GeneratedAt  time.Time `json:"generated_at"`

	// Self-improvement metrics
	TotalFindings      int `json:"total_findings"`
	RelevantFindings   int `json:"relevant_findings"`
	PRsCreated         int `json:"prs_created"`
	PRsMerged          int `json:"prs_merged"`
	SourcesScraped     int `json:"sources_scraped"`
	SuccessfulRuns     int `json:"successful_runs"`
	FailedRuns         int `json:"failed_runs"`

	// Opportunity metrics
	NewOpportunities   int     `json:"new_opportunities"`
	ProposalsDrafted   int     `json:"proposals_drafted"`
	ProposalsSent      int     `json:"proposals_sent"`
	GigsWon            int     `json:"gigs_won"`
	RevenueThisWeek    float64 `json:"revenue_this_week"`
	RevenueCumulative  float64 `json:"revenue_cumulative"`

	// Top items
	TopFindings      []WeeklyItem `json:"top_findings"`
	TopOpportunities []WeeklyItem `json:"top_opportunities"`
	TopCompetitors   []WeeklyItem `json:"top_competitors"`
	SecurityAlerts   []WeeklyItem `json:"security_alerts"`

	// Trends
	FindingsTrend     string `json:"findings_trend"`     // "up", "down", "stable"
	OpportunityTrend  string `json:"opportunity_trend"`
	CategoryBreakdown map[string]int `json:"category_breakdown"`

	// Action items — the most valuable part
	ActionItems []ActionItem `json:"action_items"`

	// Deep-dive topic covered this week
	DeepDiveTopic string `json:"deep_dive_topic"`
	ProjectsStudied []string `json:"projects_studied"`
}

// WeeklyItem represents a notable item from the week.
type WeeklyItem struct {
	Title     string `json:"title"`
	Source    string `json:"source"`
	Score     int    `json:"score"`
	Category  string `json:"category"`
}

// ActionItem is a concrete recommendation with reasoning.
type ActionItem struct {
	Priority    string `json:"priority"` // "high", "medium", "low"
	Action      string `json:"action"`
	Reasoning   string `json:"reasoning"`
	Category    string `json:"category"` // "revenue", "improvement", "security", "growth"
}

// IsWeeklyDigestDay returns true on Sundays (end of the week consolidation).
func IsWeeklyDigestDay(day time.Time) bool {
	return day.Weekday() == time.Sunday
}

// BuildWeeklyDigest reads the past 7 days of data and creates a consolidation.
func BuildWeeklyDigest(auditDir, opportunitiesDir string, now time.Time) WeeklyDigest {
	weekStart := now.AddDate(0, 0, -7)
	_, weekNum := now.ISOWeek()

	digest := WeeklyDigest{
		WeekEnding:        now.Format("2006-01-02"),
		WeekNumber:        weekNum,
		GeneratedAt:       now,
		CategoryBreakdown: make(map[string]int),
	}

	// Read audit entries from the past week
	auditLog := NewAuditLog(auditDir)
	weekEntries, _ := auditLog.ReadSince(weekStart)

	for _, e := range weekEntries {
		digest.TotalFindings++
		if e.Relevance >= 5 {
			digest.RelevantFindings++
		}
		if e.Disposition == "implemented" {
			digest.PRsCreated++
		}
		digest.CategoryBreakdown[e.Category]++

		// Collect top findings by relevance
		if e.Relevance >= 7 {
			digest.TopFindings = append(digest.TopFindings, WeeklyItem{
				Title:    e.Title,
				Source:   e.Source,
				Score:    e.Relevance,
				Category: e.Category,
			})
		}
	}

	// Sort top findings by score descending, keep top 5
	sort.Slice(digest.TopFindings, func(i, j int) bool {
		return digest.TopFindings[i].Score > digest.TopFindings[j].Score
	})
	if len(digest.TopFindings) > 5 {
		digest.TopFindings = digest.TopFindings[:5]
	}

	// Read run summaries for the week
	runsDir := filepath.Join(auditDir, "runs")
	for d := 0; d < 7; d++ {
		day := weekStart.AddDate(0, 0, d+1)
		date := day.Format("2006-01-02")
		rs, err := LoadRunSummary(runsDir, date)
		if err != nil {
			continue
		}
		digest.SourcesScraped += rs.SourcesScraped
		if rs.EmailSent {
			digest.SuccessfulRuns++
		}
		if len(rs.Errors) > 0 {
			digest.FailedRuns++
		}
	}

	// Read opportunities from the week
	pipelinePath := filepath.Join(opportunitiesDir, "pipeline.jsonl")
	allOpps, _ := ReadOpportunities(pipelinePath)
	for _, opp := range allOpps {
		scrapeDate := opp.ScrapedAt.Format("2006-01-02")
		if opp.ScrapedAt.After(weekStart) {
			digest.NewOpportunities++
			if opp.Status == StatusProposalDrafted || opp.Status == "sent" {
				digest.ProposalsDrafted++
			}
			if opp.Status == "sent" {
				digest.ProposalsSent++
			}
			if opp.Status == "won" {
				digest.GigsWon++
			}

			// Top opportunities
			if opp.RelevanceScore >= 7 {
				digest.TopOpportunities = append(digest.TopOpportunities, WeeklyItem{
					Title:    opp.Title,
					Source:   opp.Source,
					Score:    opp.Rank,
					Category: strings.Join(opp.Skills, ", "),
				})
			}
		}
		_ = scrapeDate
	}

	// Sort and limit top opportunities
	sort.Slice(digest.TopOpportunities, func(i, j int) bool {
		return digest.TopOpportunities[i].Score > digest.TopOpportunities[j].Score
	})
	if len(digest.TopOpportunities) > 5 {
		digest.TopOpportunities = digest.TopOpportunities[:5]
	}

	// Revenue
	revenuePath := filepath.Join(opportunitiesDir, "revenue.jsonl")
	revenueEntries, _ := ReadRevenue(revenuePath)
	digest.RevenueCumulative = TotalRevenue(revenueEntries)
	for _, re := range revenueEntries {
		if re.Date != "" {
			revenueDate, _ := time.Parse("2006-01-02", re.Date)
			if revenueDate.After(weekStart) {
				digest.RevenueThisWeek += re.Amount
			}
		}
	}

	// Deep-dive topic and projects studied
	digest.DeepDiveTopic = HistoricalTopicName(now)
	for d := 0; d < 7; d++ {
		day := weekStart.AddDate(0, 0, d+1)
		project := TrackedProjectForDay(day)
		digest.ProjectsStudied = append(digest.ProjectsStudied, project.Name)
	}

	// Generate action items
	digest.ActionItems = generateActionItems(digest)

	return digest
}

func generateActionItems(d WeeklyDigest) []ActionItem {
	var items []ActionItem

	// Revenue actions
	if d.NewOpportunities > 0 && d.ProposalsDrafted == 0 {
		items = append(items, ActionItem{
			Priority:  "high",
			Action:    fmt.Sprintf("Review %d opportunities and consider enabling active bidding (VXD_ACTIVE_BIDDING=true)", d.NewOpportunities),
			Reasoning: "Opportunities are being collected but no proposals drafted. Revenue starts when you start bidding.",
			Category:  "revenue",
		})
	}

	if d.ProposalsDrafted > 0 && d.ProposalsSent == 0 {
		items = append(items, ActionItem{
			Priority:  "high",
			Action:    fmt.Sprintf("%d proposals drafted and waiting for your review. Open the dashboard and send the best ones.", d.ProposalsDrafted),
			Reasoning: "Drafted proposals expire — clients hire someone else. Review within 24-48 hours.",
			Category:  "revenue",
		})
	}

	if d.GigsWon > 0 {
		items = append(items, ActionItem{
			Priority:  "medium",
			Action:    fmt.Sprintf("Congratulations on winning %d gigs! Request testimonials when work is delivered — social proof compounds.", d.GigsWon),
			Reasoning: "Each testimonial increases win rate on future proposals by ~15%.",
			Category:  "growth",
		})
	}

	// Improvement actions
	if d.PRsCreated > 3 {
		items = append(items, ActionItem{
			Priority:  "medium",
			Action:    fmt.Sprintf("Review and merge %d auto-generated PRs. Each one makes VXD more competitive.", d.PRsCreated),
			Reasoning: "Auto-improvements compound but only if merged. Stale PRs add noise.",
			Category:  "improvement",
		})
	}

	if d.FailedRuns > 2 {
		items = append(items, ActionItem{
			Priority:  "high",
			Action:    fmt.Sprintf("%d failed runs this week. Check ~/.vxd/self-improve/launchd.log for errors.", d.FailedRuns),
			Reasoning: "Failed runs mean missed opportunities and missed improvements. Fix the root cause.",
			Category:  "improvement",
		})
	}

	// Security actions
	securityCount := d.CategoryBreakdown["security"]
	if securityCount > 0 {
		items = append(items, ActionItem{
			Priority:  "high",
			Action:    fmt.Sprintf("%d security findings this week. Review them in the daily emails — especially CVEs affecting Go dependencies.", securityCount),
			Reasoning: "Security vulnerabilities in dependencies can affect client work. Stay ahead.",
			Category:  "security",
		})
	}

	// Growth actions
	if d.SuccessfulRuns >= 7 {
		items = append(items, ActionItem{
			Priority:  "low",
			Action:    "VXD ran every day this week without issues. Consider expanding the source list or increasing max findings.",
			Reasoning: "Stable system = room to grow. More sources = more signal.",
			Category:  "growth",
		})
	}

	if d.RevenueCumulative == 0 && d.NewOpportunities > 10 {
		items = append(items, ActionItem{
			Priority:  "high",
			Action:    "You've collected 10+ opportunities but revenue is still $0. Time to start bidding. Set VXD_ACTIVE_BIDDING=true.",
			Reasoning: "Data collection without action is research. Your village needs revenue, not reports.",
			Category:  "revenue",
		})
	}

	return items
}

// WeeklyEmailHTML builds the weekly digest email.
const weeklyEmailTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;max-width:700px;margin:0 auto;padding:20px;color:#333;">

<h1 style="border-bottom:3px solid #7c3aed;padding-bottom:10px;color:#7c3aed;">VXD Weekly Digest</h1>
<p style="color:#666;">Week {{.WeekNumber}} — ending {{.WeekEnding}}</p>

<div style="display:flex;gap:12px;margin-bottom:20px;flex-wrap:wrap;">
<div style="flex:1;min-width:140px;padding:12px;background:#f0fdf4;border-radius:8px;text-align:center;">
<div style="font-size:1.8rem;font-weight:700;color:#16a34a;">{{.PRsCreated}}</div>
<div style="font-size:0.8rem;color:#666;">PRs Created</div>
</div>
<div style="flex:1;min-width:140px;padding:12px;background:#eff6ff;border-radius:8px;text-align:center;">
<div style="font-size:1.8rem;font-weight:700;color:#2563eb;">{{.RelevantFindings}}</div>
<div style="font-size:0.8rem;color:#666;">Relevant Findings</div>
</div>
<div style="flex:1;min-width:140px;padding:12px;background:#fefce8;border-radius:8px;text-align:center;">
<div style="font-size:1.8rem;font-weight:700;color:#ca8a04;">{{.NewOpportunities}}</div>
<div style="font-size:0.8rem;color:#666;">Opportunities</div>
</div>
<div style="flex:1;min-width:140px;padding:12px;background:#faf5ff;border-radius:8px;text-align:center;">
<div style="font-size:1.8rem;font-weight:700;color:#7c3aed;">${{printf "%.0f" .RevenueCumulative}}</div>
<div style="font-size:0.8rem;color:#666;">Total Revenue</div>
</div>
</div>

{{if .ActionItems}}
<a name="actions"></a>
<div style="margin-bottom:25px;padding:15px;background:#fef2f2;border-radius:8px;border-left:4px solid #dc2626;">
<h2 style="color:#dc2626;margin:0 0 10px 0;">Action Items This Week</h2>
{{range .ActionItems}}
<div style="margin-bottom:10px;padding:8px;background:white;border-radius:6px;">
<strong style="color:{{if eq .Priority "high"}}#dc2626{{else if eq .Priority "medium"}}#ca8a04{{else}}#2563eb{{end}};">[{{.Priority}}]</strong>
<strong>{{.Action}}</strong>
<p style="margin:4px 0 0;font-size:0.85rem;color:#64748b;">{{.Reasoning}}</p>
</div>
{{end}}
</div>
{{end}}

{{if .TopFindings}}
<a name="findings"></a>
<div style="margin-bottom:25px;">
<h2 style="color:#2563eb;border-bottom:2px solid #2563eb;padding-bottom:5px;">Top Findings This Week</h2>
{{range .TopFindings}}
<div style="margin-bottom:8px;padding:8px;background:#f8fafc;border-radius:6px;">
<strong>{{.Title}}</strong>
<span style="float:right;font-size:0.8rem;color:#64748b;">Score: {{.Score}} | {{.Category}}</span>
</div>
{{end}}
</div>
{{end}}

{{if .TopOpportunities}}
<a name="opportunities"></a>
<div style="margin-bottom:25px;">
<h2 style="color:#059669;border-bottom:2px solid #059669;padding-bottom:5px;">Top Opportunities This Week</h2>
{{range .TopOpportunities}}
<div style="margin-bottom:8px;padding:8px;background:#ecfdf5;border-radius:6px;">
<strong>{{.Title}}</strong>
<span style="float:right;font-size:0.8rem;color:#64748b;">Rank: {{.Score}} | {{.Category}}</span>
</div>
{{end}}
</div>
{{end}}

<div style="margin-bottom:25px;">
<h2 style="color:#7c3aed;border-bottom:2px solid #7c3aed;padding-bottom:5px;">This Week's Research</h2>
<p><strong>Deep-dive topic:</strong> {{.DeepDiveTopic}}</p>
<p><strong>Projects studied:</strong> {{range $i, $p := .ProjectsStudied}}{{if $i}}, {{end}}{{$p}}{{end}}</p>
<p><strong>Sources scraped:</strong> {{.SourcesScraped}} | <strong>Runs:</strong> {{.SuccessfulRuns}} successful, {{.FailedRuns}} failed</p>
</div>

{{if gt .RevenueCumulative 0.0}}
<div style="margin-bottom:25px;padding:15px;background:linear-gradient(135deg,#fef3c7,#fde68a);border-radius:8px;">
<h2 style="color:#92400e;margin:0 0 8px 0;">Revenue: ${{printf "%.0f" .RevenueCumulative}}</h2>
<p style="color:#78350f;margin:0;">This week: ${{printf "%.0f" .RevenueThisWeek}} | Gigs won: {{.GigsWon}}</p>
<p style="color:#78350f;margin:4px 0 0;font-style:italic;">Remember your mission: schools, children, infrastructure. Every dollar compounds toward transformation.</p>
</div>
{{end}}

<hr style="margin-top:30px;border:1px solid #e5e7eb;">

<table style="width:100%;border-collapse:collapse;margin-top:20px;">
<tr>
<td style="width:60px;vertical-align:top;padding-right:15px;">
<div style="width:50px;height:50px;background:linear-gradient(135deg,#2563eb,#7c3aed);border-radius:12px;">
<span style="color:white;font-weight:800;font-size:20px;font-family:monospace;display:block;text-align:center;line-height:50px;">VXD</span>
</div>
</td>
<td style="vertical-align:top;">
<p style="margin:0;font-weight:700;color:#1e293b;font-size:0.95rem;">Vortex Dispatch</p>
<p style="margin:2px 0;color:#64748b;font-size:0.8rem;">AI-Augmented Software Development</p>
<p style="margin:6px 0 0;font-size:0.8rem;">
<a href="https://github.com/tzone85/vortex-dispatch" style="color:#2563eb;text-decoration:none;margin-right:12px;">GitHub</a>
<a href="mailto:vortex.dispatch01@gmail.com" style="color:#2563eb;text-decoration:none;">Email</a>
</p>
</td>
</tr>
</table>

</body></html>`

// BuildWeeklyEmailHTML renders the weekly digest email.
func BuildWeeklyEmailHTML(digest WeeklyDigest) (string, error) {
	tmpl, err := template.New("weekly").Parse(weeklyEmailTemplate)
	if err != nil {
		return "", fmt.Errorf("parse weekly template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, digest); err != nil {
		return "", fmt.Errorf("execute weekly template: %w", err)
	}
	return buf.String(), nil
}
