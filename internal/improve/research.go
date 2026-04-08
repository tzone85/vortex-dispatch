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

// DeepDiveTopic represents a topic that gets 5 days of progressive research.
type DeepDiveTopic struct {
	Name   string
	Phases []string // 5 search queries, one per day
}

var deepDiveTopics = []DeepDiveTopic{
	{Name: "Event Sourcing", Phases: []string{
		"event sourcing CQRS best practices 2024 2025",
		"event sourcing Go implementation patterns examples",
		"event sourcing pitfalls mistakes lessons learned",
		"event sourcing projection rebuild replay strategies",
		"event sourcing testing strategies snapshot optimization",
	}},
	{Name: "AI Agent Orchestration", Phases: []string{
		"AI agent orchestration multi-agent systems architecture",
		"AI agent orchestration frameworks comparison LangGraph CrewAI AutoGen",
		"AI agent orchestration error recovery retry patterns",
		"AI agent orchestration cost optimization token management",
		"AI agent orchestration evaluation benchmarks testing",
	}},
	{Name: "CLI UX Patterns", Phases: []string{
		"CLI UX design best practices Go Cobra 2024",
		"terminal user interface TUI patterns Go Bubbletea Charm",
		"CLI progress indicators spinners real-time output patterns",
		"CLI error messages user-friendly patterns examples",
		"CLI configuration management patterns dotfiles YAML TOML",
	}},
	{Name: "Go Performance", Phases: []string{
		"Go performance optimization profiling pprof 2024",
		"Go concurrency patterns goroutine pool worker patterns",
		"Go memory optimization reduce allocations escape analysis",
		"Go HTTP server performance optimization middleware",
		"Go SQLite performance WAL mode concurrent access patterns",
	}},
	{Name: "LLM Prompt Engineering", Phases: []string{
		"LLM structured output JSON function calling best practices",
		"LLM prompt injection defense detection mitigation 2024",
		"LLM cost optimization prompt caching batching strategies",
		"LLM evaluation harness automated testing prompts",
		"LLM multi-model routing fallback strategies architecture",
	}},
	{Name: "Security Hardening", Phases: []string{
		"Go application security hardening OWASP checklist 2024",
		"supply chain security Go modules dependency vulnerability",
		"secret management environment variables Go applications",
		"API key rotation automated secret scanning CI CD",
		"Go fuzzing testing security vulnerability discovery",
	}},
	{Name: "Code Review Automation", Phases: []string{
		"automated code review tools AI-powered 2024 2025",
		"static analysis Go golangci-lint custom rules patterns",
		"code review checklist quality gates CI pipeline",
		"AI code review accuracy false positive reduction",
		"merge conflict resolution automation strategies",
	}},
	{Name: "Testing Strategies", Phases: []string{
		"testing strategies AI systems non-deterministic output",
		"Go integration testing testcontainers httptest patterns",
		"test-driven development Go best practices 2024",
		"flaky test detection quarantine strategies CI",
		"mutation testing Go code coverage beyond line coverage",
	}},
}

// TrackedProject represents a named project to study for ideas.
type TrackedProject struct {
	Name     string
	URLs     []string // GitHub repo, docs, blog — rotated across days
	Category string
}

var trackedProjects = []TrackedProject{
	{Name: "Gas Town", URLs: []string{
		"https://github.com/gastownhall/gastown",
		"https://github.com/gastownhall/gastown/releases",
	}, Category: "historical"},
	{Name: "Dagger CI", URLs: []string{
		"https://github.com/dagger/dagger/releases",
		"https://docs.dagger.io/",
	}, Category: "competitors"},
	{Name: "Taskfile", URLs: []string{
		"https://github.com/go-task/task/releases",
		"https://taskfile.dev/",
	}, Category: "go_ecosystem"},
	{Name: "Charm Tools", URLs: []string{
		"https://github.com/charmbracelet/bubbletea/releases",
		"https://charm.sh/blog/",
	}, Category: "go_ecosystem"},
	{Name: "LangChain", URLs: []string{
		"https://github.com/langchain-ai/langchain/releases",
		"https://blog.langchain.dev/",
	}, Category: "competitors"},
	{Name: "AutoGen", URLs: []string{
		"https://github.com/microsoft/autogen/releases",
	}, Category: "competitors"},
	{Name: "CrewAI", URLs: []string{
		"https://github.com/crewAIInc/crewAI/releases",
	}, Category: "competitors"},
	{Name: "Aider", URLs: []string{
		"https://github.com/paul-gauthier/aider/releases",
		"https://aider.chat/blog/",
	}, Category: "competitors"},
	{Name: "GoReleaser", URLs: []string{
		"https://github.com/goreleaser/goreleaser/releases",
	}, Category: "go_ecosystem"},
	{Name: "Event Store", URLs: []string{
		"https://www.eventstore.com/blog",
	}, Category: "historical"},
	{Name: "MemPalace", URLs: []string{
		"https://github.com/milla-jovovich/mempalace",
		"https://github.com/milla-jovovich/mempalace/releases",
	}, Category: "go_ecosystem"},
	{Name: "DeerFlow", URLs: []string{
		"https://deerflow.tech",
	}, Category: "competitors"},
	{Name: "Hungry Ghost Hive", URLs: []string{
		"https://github.com/tzone85/hungry-ghost-hive",
	}, Category: "historical"},
	{Name: "Wasteland", URLs: []string{
		"https://github.com/gastownhall/wasteland",
		"https://github.com/gastownhall/wasteland/releases",
	}, Category: "historical"},
	{Name: "Natural20 Blog", URLs: []string{
		"https://natural20.beehiiv.com/p/claude-code-got-cloned-in-2-hours",
		"https://natural20.beehiiv.com/p/anthropic-says-its-new-ai-model-is-too-dangerous-to-release",
	}, Category: "llm_providers"},
	{Name: "Anthropic Glasswing", URLs: []string{
		"https://www.anthropic.com/glasswing",
	}, Category: "llm_providers"},
	{Name: "Claude Mythos Cybersecurity", URLs: []string{
		"https://thenewstack.io/anthropic-claude-mythos-cybersecurity/",
		"https://red.anthropic.com/2026/mythos-preview/",
	}, Category: "security"},
	{Name: "Claw Code", URLs: []string{
		"https://github.com/ultraworkers/claw-code",
		"https://github.com/ultraworkers/claw-code/releases",
	}, Category: "competitors"},
	{Name: "OpenAI Developer Blog", URLs: []string{
		"https://developers.openai.com/blog",
		"https://developers.openai.com/api/docs/changelog",
	}, Category: "competitors"},
	{Name: "ChatGPT Release Notes", URLs: []string{
		"https://help.openai.com/en/articles/6825453-chatgpt-release-notes",
	}, Category: "competitors"},
	{Name: "OpenAI Codex CLI", URLs: []string{
		"https://github.com/openai/codex",
	}, Category: "competitors"},
	{Name: "OpenAI Agents SDK", URLs: []string{
		"https://github.com/openai/openai-agents-python",
	}, Category: "competitors"},
	{Name: "Warp Terminal", URLs: []string{
		"https://github.com/warpdotdev/Warp",
		"https://www.warp.dev/blog",
		"https://docs.warp.dev/changelog",
	}, Category: "competitors"},
	{Name: "Amazon Bedrock", URLs: []string{
		"https://aws.amazon.com/bedrock/",
		"https://aws.amazon.com/blogs/aws/category/artificial-intelligence/amazon-machine-learning/amazon-bedrock/",
		"https://github.com/aws-samples/amazon-bedrock-samples",
	}, Category: "llm_providers"},
}

// HistoricalTopicForDay returns the deep-dive search query for today.
// Each topic gets 5 consecutive days of progressive research before moving to the next.
func HistoricalTopicForDay(day time.Time) string {
	topicIdx := (day.YearDay() / 5) % len(deepDiveTopics)
	phaseIdx := day.YearDay() % 5
	topic := deepDiveTopics[topicIdx]
	if phaseIdx < len(topic.Phases) {
		return topic.Phases[phaseIdx]
	}
	return topic.Phases[0]
}

// HistoricalTopicName returns the current topic name (for logging/email).
func HistoricalTopicName(day time.Time) string {
	topicIdx := (day.YearDay() / 5) % len(deepDiveTopics)
	return deepDiveTopics[topicIdx].Name
}

// TrackedProjectForDay returns the project to study today, rotating through the list.
func TrackedProjectForDay(day time.Time) TrackedProject {
	idx := day.YearDay() % len(trackedProjects)
	return trackedProjects[idx]
}

// TrackedProjectURLForDay returns a specific URL from the tracked project for today.
func TrackedProjectURLForDay(day time.Time) (TrackedProject, string) {
	project := TrackedProjectForDay(day)
	urlIdx := day.YearDay() % len(project.URLs)
	return project, project.URLs[urlIdx]
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
	total := len(currentSources) + 2 // +1 deep-dive + 1 tracked project

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

	// Progressive deep-dive: 5 days per topic, each day goes deeper
	topicName := HistoricalTopicName(now)
	topicQuery := HistoricalTopicForDay(now)
	phase := now.YearDay()%5 + 1
	log.Printf("  [%d/%d] Deep-dive: %s (day %d/5) ...", total-1, total, topicName, phase)
	historicalSrc := Source{
		Name:      fmt.Sprintf("Deep-dive: %s (day %d/5)", topicName, phase),
		URL:       "https://www.google.com/search?q=" + topicQuery,
		Category:  "historical",
		Direction: "historical",
	}
	start := time.Now()
	f, err := r.scrape(ctx, historicalSrc, now)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%d/%d] Deep-dive FAILED (%s): %v", total-1, total, elapsed, err)
	} else {
		log.Printf("  [%d/%d] Deep-dive OK (%s)", total-1, total, elapsed)
		findings = append(findings, f...)
	}

	// Tracked project study: rotate through named projects
	project, projectURL := TrackedProjectURLForDay(now)
	log.Printf("  [%d/%d] Studying project: %s ...", total, total, project.Name)
	projectSrc := Source{
		Name:      "Project: " + project.Name,
		URL:       projectURL,
		Category:  project.Category,
		Direction: "historical",
	}
	start = time.Now()
	f, err = r.scrape(ctx, projectSrc, now)
	elapsed = time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("  [%d/%d] Project %s FAILED (%s): %v", total, total, project.Name, elapsed, err)
	} else {
		log.Printf("  [%d/%d] Project %s OK (%s)", total, total, project.Name, elapsed)
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
