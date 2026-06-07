package improve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// EmailData holds all data needed to build the report email.
type EmailData struct {
	Date           string
	PRsCreated     int
	AlertCount     int
	Summary        string
	PRs            []EmailPR
	Trends         []EmailSection
	Historical     []EmailSection
	Competitors    []EmailSection
	SecurityAlerts []EmailSection
	Proposed         []EmailSection
	ChartURLs        map[string]string
	Opportunities    []EmailOpportunity
	OpportunityStats *OpportunityStats
	MissionMilestone *MissionMilestoneData
}

// EmailPR represents a PR in the email table.
type EmailPR struct {
	Title        string
	URL          string
	Category     string
	TestsPassed  bool
	LinesChanged int
}

// EmailSection represents a content section in the email.
type EmailSection struct {
	Title     string
	Content   string
	SourceURL string
}

// EmailOpportunity represents an opportunity in the email table.
type EmailOpportunity struct {
	Title    string
	URL      string
	Source   string
	Budget   string
	Rank     int
	Status   string
	HasDraft bool
}

// OpportunityStats holds aggregate stats for the email.
type OpportunityStats struct {
	TotalPipeline    int
	NewToday         int
	ProposalsDrafted int
	TotalRevenue     float64
}

// MissionMilestoneData holds milestone reminder data for the email.
type MissionMilestoneData struct {
	Amount  float64
	Message string
}

const emailTemplateSrc = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;max-width:700px;margin:0 auto;padding:20px;color:#333;">

<h1 style="border-bottom:3px solid #2563eb;padding-bottom:10px;">VXD Daily Improvement Report</h1>
<p style="color:#666;">{{.Date}}</p>

<div style="margin-bottom:20px;padding:10px;background:#f0f9ff;border-left:4px solid #2563eb;">
{{if .PRsCreated}}<strong>{{.PRsCreated}} PRs created</strong>{{end}}
{{if .AlertCount}} | <strong style="color:#dc2626;">{{.AlertCount}} security alerts</strong>{{end}}
</div>

<nav style="margin-bottom:20px;">
<a href="#summary" style="margin-right:12px;">Summary</a>
{{if .PRs}}<a href="#prs" style="margin-right:12px;">PRs</a>{{end}}
{{if .Trends}}<a href="#trends" style="margin-right:12px;">Trends</a>{{end}}
{{if .Historical}}<a href="#historical" style="margin-right:12px;">Historical</a>{{end}}
{{if .Competitors}}<a href="#competitors" style="margin-right:12px;">Competitors</a>{{end}}
{{if .SecurityAlerts}}<a href="#security" style="margin-right:12px;">Security</a>{{end}}
{{if .Proposed}}<a href="#proposed" style="margin-right:12px;">Proposed</a>{{end}}
{{if .Opportunities}}<a href="#opportunities" style="margin-right:12px;">Opportunities</a>{{end}}
</nav>

<a name="summary"></a><div style="margin-bottom:30px;">
<h2 style="color:#2563eb;">Executive Summary</h2>
<p>{{.Summary}}</p>
</div>

{{if .PRs}}
<a name="prs"></a><div style="margin-bottom:30px;">
<h2 style="color:#16a34a;border-bottom:2px solid #16a34a;padding-bottom:5px;">PRs Created Today</h2>
<table style="width:100%;border-collapse:collapse;">
<tr style="background:#f0fdf4;"><th style="padding:8px;text-align:left;">Title</th><th>Category</th><th>Tests</th><th>Lines</th></tr>
{{range .PRs}}
<tr style="border-bottom:1px solid #e5e7eb;">
<td style="padding:8px;"><a href="{{.URL}}">{{.Title}}</a></td>
<td style="padding:8px;text-align:center;">{{.Category}}</td>
<td style="padding:8px;text-align:center;">{{if .TestsPassed}}PASS{{else}}FAIL{{end}}</td>
<td style="padding:8px;text-align:center;">{{.LinesChanged}}</td>
</tr>
{{end}}
</table>
</div>
{{end}}

{{if .Trends}}
<a name="trends"></a><div style="margin-bottom:30px;">
<h2 style="color:#2563eb;border-bottom:2px solid #2563eb;padding-bottom:5px;">Current Trends</h2>
{{range .Trends}}
<div style="margin-bottom:15px;padding:10px;background:#f8fafc;border-radius:6px;">
<strong>{{.Title}}</strong>
<p style="margin:5px 0;">{{.Content}}</p>
{{if .SourceURL}}<a href="{{.SourceURL}}" style="font-size:0.85em;">Source</a>{{end}}
</div>
{{end}}
</div>
{{end}}

{{if .Historical}}
<a name="historical"></a><div style="margin-bottom:30px;">
<h2 style="color:#7c3aed;border-bottom:2px solid #7c3aed;padding-bottom:5px;">Historical Discoveries</h2>
{{range .Historical}}
<div style="margin-bottom:15px;padding:10px;background:#faf5ff;border-radius:6px;">
<strong>{{.Title}}</strong>
<p style="margin:5px 0;">{{.Content}}</p>
{{if .SourceURL}}<a href="{{.SourceURL}}" style="font-size:0.85em;">Source</a>{{end}}
</div>
{{end}}
</div>
{{end}}

{{if .Competitors}}
<a name="competitors"></a><div style="margin-bottom:30px;">
<h2 style="color:#ea580c;border-bottom:2px solid #ea580c;padding-bottom:5px;">Competitor Watch</h2>
{{range .Competitors}}
<div style="margin-bottom:15px;padding:10px;background:#fff7ed;border-radius:6px;">
<strong>{{.Title}}</strong>
<p style="margin:5px 0;">{{.Content}}</p>
{{if .SourceURL}}<a href="{{.SourceURL}}" style="font-size:0.85em;">Source</a>{{end}}
</div>
{{end}}
</div>
{{end}}

{{if .SecurityAlerts}}
<a name="security"></a><div style="margin-bottom:30px;">
<h2 style="color:#dc2626;border-bottom:2px solid #dc2626;padding-bottom:5px;">Security Alerts</h2>
{{range .SecurityAlerts}}
<div style="margin-bottom:15px;padding:10px;background:#fef2f2;border-radius:6px;border-left:4px solid #dc2626;">
<strong>{{.Title}}</strong>
<p style="margin:5px 0;">{{.Content}}</p>
{{if .SourceURL}}<a href="{{.SourceURL}}" style="font-size:0.85em;">Source</a>{{end}}
</div>
{{end}}
</div>
{{end}}

{{if .ChartURLs}}
<div style="margin-bottom:30px;">
<h2 style="color:#2563eb;">Metrics Dashboard</h2>
{{range $name, $url := .ChartURLs}}
<img src="{{$url}}" alt="{{$name}}" style="max-width:100%;margin-bottom:10px;">
{{end}}
</div>
{{end}}

{{if .Proposed}}
<a name="proposed"></a><div style="margin-bottom:30px;">
<h2 style="color:#ca8a04;border-bottom:2px solid #ca8a04;padding-bottom:5px;">Proposed (Not Implemented)</h2>
<p style="color:#666;">These improvements were too large or risky for auto-implementation. Review and decide.</p>
{{range .Proposed}}
<div style="margin-bottom:15px;padding:10px;background:#fefce8;border-radius:6px;">
<strong>{{.Title}}</strong>
<p style="margin:5px 0;">{{.Content}}</p>
{{if .SourceURL}}<a href="{{.SourceURL}}" style="font-size:0.85em;">Source</a>{{end}}
</div>
{{end}}
</div>
{{end}}

{{if .Opportunities}}
<a name="opportunities"></a><div style="margin-bottom:30px;">
<h2 style="color:#059669;border-bottom:2px solid #059669;padding-bottom:5px;">Opportunities Found Today</h2>
{{if .OpportunityStats}}
<div style="padding:8px;background:#ecfdf5;border-radius:6px;margin-bottom:12px;color:#333;">
<strong>{{.OpportunityStats.NewToday}} new</strong>
{{if .OpportunityStats.ProposalsDrafted}} | <strong>{{.OpportunityStats.ProposalsDrafted}} proposals drafted</strong>{{end}}
 | Pipeline: {{.OpportunityStats.TotalPipeline}} total
{{if .OpportunityStats.TotalRevenue}} | Revenue: ${{printf "%.0f" .OpportunityStats.TotalRevenue}}{{end}}
</div>
{{end}}
<table style="width:100%;border-collapse:collapse;">
<tr style="background:#ecfdf5;"><th style="padding:8px;text-align:left;color:#333;">Title</th><th style="color:#333;">Source</th><th style="color:#333;">Budget</th><th style="color:#333;">Rank</th><th style="color:#333;">Status</th></tr>
{{range .Opportunities}}
<tr style="border-bottom:1px solid #e5e7eb;">
<td style="padding:8px;">{{if .URL}}<a href="{{.URL}}" style="color:#059669;text-decoration:none;font-weight:600;" target="_blank">{{.Title}}</a>{{else}}{{.Title}}{{end}}</td>
<td style="padding:8px;text-align:center;">{{.Source}}</td>
<td style="padding:8px;text-align:center;">{{.Budget}}</td>
<td style="padding:8px;text-align:center;">{{.Rank}}</td>
<td style="padding:8px;text-align:center;">{{if .HasDraft}}&#x2709; {{end}}{{.Status}}</td>
</tr>
{{end}}
</table>
<p style="font-size:0.85em;color:#666;">Open dashboard: <a href="http://localhost:8078">http://localhost:8078</a></p>
</div>
{{end}}

{{if .MissionMilestone}}
<div style="margin-bottom:30px;padding:15px;background:linear-gradient(135deg,#fef3c7,#fde68a);border-radius:8px;border-left:4px solid #f59e0b;">
<h2 style="color:#92400e;margin:0 0 8px 0;">Mission Milestone: ${{printf "%.0f" .MissionMilestone.Amount}}</h2>
<p style="color:#78350f;margin:0;line-height:1.6;">{{.MissionMilestone.Message}} Schools need funding. Children need resources. Infrastructure needs building. This is the compound working. What's your next impact move?</p>
</div>
{{end}}

<hr style="margin-top:30px;border:1px solid #e5e7eb;">

<table style="width:100%;border-collapse:collapse;margin-top:20px;">
<tr>
<td style="width:60px;vertical-align:top;padding-right:15px;">
<div style="width:50px;height:50px;background:linear-gradient(135deg,#2563eb,#7c3aed);border-radius:12px;display:flex;align-items:center;justify-content:center;">
<span style="color:white;font-weight:800;font-size:20px;font-family:monospace;display:block;text-align:center;line-height:50px;">VXD</span>
</div>
</td>
<td style="vertical-align:top;">
<p style="margin:0;font-weight:700;color:#1e293b;font-size:0.95rem;">Vortex Dispatch</p>
<p style="margin:2px 0;color:#64748b;font-size:0.8rem;">AI-Augmented Software Development</p>
<p style="margin:6px 0 0;font-size:0.8rem;">
<a href="https://github.com/tzone85/vortex-dispatch" style="color:#2563eb;text-decoration:none;margin-right:12px;">GitHub</a>
<a href="https://github.com/tzone85/vortex-dispatch/blob/main/docs/self-improvement/changelog.jsonl" style="color:#94a3b8;text-decoration:none;font-size:0.75rem;">Audit Trail</a>
</p>
</td>
</tr>
</table>

</body></html>`

// BuildEmailHTML renders the email template with the given data.
func BuildEmailHTML(data EmailData) (string, error) {
	tmpl, err := template.New("email").Parse(emailTemplateSrc)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// BuildChartURL constructs a QuickChart.io URL for embedding in email.
func BuildChartURL(chartType string, config map[string]any) string {
	chartConfig := map[string]any{
		"type": chartType,
		"data": config,
	}
	configJSON, _ := json.Marshal(chartConfig)
	return "https://quickchart.io/chart?" + url.Values{"c": {string(configJSON)}}.Encode()
}

// EmailSender sends HTML emails via the Resend API.
type EmailSender struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewEmailSender creates a sender configured with the Resend API key.
func NewEmailSender(apiKey, baseURL string) *EmailSender {
	return &EmailSender{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Send sends an HTML email via Resend.
func (s *EmailSender) Send(ctx context.Context, subject, html, to, from string) error {
	body := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	}
	jsonBody, _ := json.Marshal(body)

	// Handle both production URL and test server URLs
	endpoint := s.baseURL
	if strings.HasPrefix(s.baseURL, "https://api.resend.com") {
		endpoint = s.baseURL + "/emails"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("resend returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
