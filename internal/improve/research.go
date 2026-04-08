package improve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Finding represents a single research result from a scraped source.
type Finding struct {
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	SourceURL string    `json:"source_url"`
	Category  string    `json:"category"`
	Direction string    `json:"direction"`
	ScrapedAt time.Time `json:"scraped_at"`
}

// Source defines a web source to scrape for improvements.
type Source struct {
	Name      string
	URL       string
	Category  string
	Direction string
}

var currentSources = []Source{
	{Name: "Go Blog", URL: "https://go.dev/blog/", Category: "go_ecosystem", Direction: "current"},
	{Name: "Go Releases", URL: "https://go.dev/doc/devel/release", Category: "go_ecosystem", Direction: "current"},
	{Name: "Anthropic News", URL: "https://www.anthropic.com/news", Category: "llm_providers", Direction: "current"},
	{Name: "OpenAI Blog", URL: "https://openai.com/blog", Category: "llm_providers", Direction: "current"},
	{Name: "Google AI Blog", URL: "https://blog.google/technology/ai/", Category: "llm_providers", Direction: "current"},
	{Name: "Ollama Releases", URL: "https://github.com/ollama/ollama/releases", Category: "llm_providers", Direction: "current"},
	{Name: "Go Vuln DB", URL: "https://vuln.go.dev/", Category: "security", Direction: "current"},
	{Name: "HN Best", URL: "https://news.ycombinator.com/best", Category: "general_se", Direction: "current"},
	{Name: "SWE-agent Releases", URL: "https://github.com/princeton-nlp/SWE-agent/releases", Category: "competitors", Direction: "current"},
	{Name: "OpenHands Releases", URL: "https://github.com/All-Hands-AI/OpenHands/releases", Category: "competitors", Direction: "current"},
}

var historicalTopics = []string{
	"event sourcing patterns best practices",
	"tmux automation scripting techniques",
	"CLI UX design patterns Go",
	"AI agent orchestration architecture",
	"Go performance optimization profiling",
	"distributed systems design patterns",
	"LLM prompt engineering structured output",
	"automated code review tools techniques",
	"dependency management security Go",
	"testing strategies AI systems",
}

// HistoricalTopicForDay returns the historical deep-dive topic for a given day.
func HistoricalTopicForDay(day time.Time) string {
	idx := day.YearDay() % len(historicalTopics)
	return historicalTopics[idx]
}

// Researcher scrapes web sources via the Firecrawl API.
type Researcher struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewResearcher creates a Researcher configured with the Firecrawl API key.
func NewResearcher(apiKey, baseURL string) *Researcher {
	return &Researcher{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Research scrapes all configured sources and returns sanitized, filtered findings.
func (r *Researcher) Research(ctx context.Context, now time.Time) ([]Finding, error) {
	var findings []Finding
	total := len(currentSources) + 1 // +1 for historical

	for i, src := range currentSources {
		log.Printf("  [%d/%d] Scraping %s ...", i+1, total, src.Name)
		start := time.Now()
		f, err := r.scrape(ctx, src, now)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.Printf("  [%d/%d] %s FAILED (%s): %v", i+1, total, src.Name, elapsed, err)
			continue
		}
		log.Printf("  [%d/%d] %s OK (%s, %d chars)", i+1, total, src.Name, elapsed, len(f[0].Content))
		findings = append(findings, f...)
	}

	topic := HistoricalTopicForDay(now)
	log.Printf("  [%d/%d] Scraping Historical: %s ...", total, total, topic)
	historicalSrc := Source{
		Name:      "Historical: " + topic,
		URL:       "https://www.google.com/search?q=" + topic,
		Category:  "historical",
		Direction: "historical",
	}
	start := time.Now()
	f, err := r.scrape(ctx, historicalSrc, now)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%d/%d] Historical FAILED (%s): %v", total, total, elapsed, err)
	} else {
		log.Printf("  [%d/%d] Historical OK (%s)", total, total, elapsed)
		findings = append(findings, f...)
	}

	safe := make([]Finding, 0, len(findings))
	filtered := 0
	for _, f := range findings {
		if DetectPromptInjection(f.Content) || DetectPromptInjection(f.Title) {
			filtered++
			log.Printf("  Filtered injection in %q from %s", f.Title, f.SourceURL)
			continue
		}
		safe = append(safe, f)
	}
	if filtered > 0 {
		log.Printf("  Filtered %d findings with prompt injection patterns", filtered)
	}

	return safe, nil
}

type firecrawlRequest struct {
	URL     string   `json:"url"`
	Formats []string `json:"formats"`
}

type firecrawlResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Markdown string `json:"markdown"`
		Metadata struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"metadata"`
	} `json:"data"`
}

func (r *Researcher) scrape(ctx context.Context, src Source, now time.Time) ([]Finding, error) {
	body := firecrawlRequest{URL: src.URL, Formats: []string{"markdown"}}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := r.baseURL + "/v2/scrape"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firecrawl returned %d: %s", resp.StatusCode, string(respBody))
	}

	var fcResp firecrawlResponse
	if err := json.Unmarshal(respBody, &fcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !fcResp.Success || fcResp.Data.Markdown == "" {
		return nil, fmt.Errorf("firecrawl returned empty or failed response")
	}

	title := fcResp.Data.Metadata.Title
	if title == "" {
		title = src.Name
	}

	return []Finding{{
		Title:     SanitizeContent(title),
		Content:   SanitizeContent(fcResp.Data.Markdown),
		SourceURL: src.URL,
		Category:  src.Category,
		Direction: src.Direction,
		ScrapedAt: now,
	}}, nil
}
