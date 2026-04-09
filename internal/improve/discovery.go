package improve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// SourceDiscoverer uses Gemma 4 and Firecrawl to find new opportunity sources.
type SourceDiscoverer struct {
	llmClient    llm.Client
	firecrawlKey string
	firecrawlURL string
	dataDir      string
}

// NewSourceDiscoverer creates a source discoverer.
func NewSourceDiscoverer(client llm.Client, firecrawlKey, firecrawlURL, dataDir string) *SourceDiscoverer {
	return &SourceDiscoverer{
		llmClient:    client,
		firecrawlKey: firecrawlKey,
		firecrawlURL: firecrawlURL,
		dataDir:      dataDir,
	}
}

// IsDiscoveryDay returns true every 7th run (weekly cycle).
func IsDiscoveryDay(runCount int) bool {
	return runCount > 0 && runCount%7 == 0
}

// DiscoverNewSources asks Gemma 4 to suggest new opportunity sources based on
// the week's top skills, then verifies each with Firecrawl.
func (d *SourceDiscoverer) DiscoverNewSources(ctx context.Context, topSkills []string) ([]DiscoveredSource, error) {
	log.Printf("  [discovery] Analyzing week's data to suggest new sources ...")

	prompt := fmt.Sprintf(`You are analyzing freelance opportunity data for an AI-augmented development team. The team's most in-demand skills this week were: %s

Suggest exactly 3 NEW freelance/job/bounty websites that might have relevant opportunities. Focus on:
- Sites with software development freelance work
- Bounty platforms for open source contributions
- Niche job boards for remote developers
- Community forums with hiring threads

Respond with JSON only:
{"sources": [
  {"url": "https://...", "name": "Site Name", "reason": "Why this source is valuable"},
  {"url": "https://...", "name": "Site Name", "reason": "Why this source is valuable"},
  {"url": "https://...", "name": "Site Name", "reason": "Why this source is valuable"}
]}`, strings.Join(topSkills, ", "))

	resp, err := d.llmClient.Complete(ctx, llm.CompletionRequest{
		Model:     "gemma-4-26b-a4b-it",
		MaxTokens: 1000,
		System:    "You are a market research analyst finding new freelance opportunity sources for a software development team. Respond with JSON only.",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("gemma 4 discovery: %w", err)
	}

	var result struct {
		Sources []struct {
			URL    string `json:"url"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"sources"`
	}

	cleaned := strings.TrimSpace(resp.Content)
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse discovery response: %w", err)
	}

	var discovered []DiscoveredSource
	today := time.Now().Format("2006-01-02")

	for _, s := range result.Sources {
		log.Printf("  [discovery] Verifying %s (%s) ...", s.Name, s.URL)

		// Verify the source contains job listings via Firecrawl
		verified := d.verifySource(ctx, s.URL)
		if !verified {
			log.Printf("  [discovery] %s did not contain job listings, skipping", s.Name)
			continue
		}

		src := DiscoveredSource{
			URL:          s.URL,
			Name:         s.Name,
			DiscoveredOn: today,
			Reason:       s.Reason,
			Status:       "pending_approval",
		}
		discovered = append(discovered, src)

		// Persist to JSONL
		sourcesPath := d.sourcesPath()
		if err := AppendDiscoveredSource(sourcesPath, src); err != nil {
			log.Printf("  [discovery] Failed to save source %s: %v", s.Name, err)
		}

		log.Printf("  [discovery] Discovered: %s — %s", s.Name, s.Reason)
	}

	log.Printf("  [discovery] %d new sources discovered (pending approval)", len(discovered))
	return discovered, nil
}

// verifySource checks if a URL contains job listings using Firecrawl.
func (d *SourceDiscoverer) verifySource(ctx context.Context, url string) bool {
	if d.firecrawlKey == "" {
		return true // Skip verification if no Firecrawl key
	}

	body := map[string]any{
		"url":     url,
		"formats": []string{"markdown"},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return false
	}

	endpoint := d.firecrawlURL + "/v2/scrape"
	req, err := newRequestWithContext(ctx, endpoint, jsonBody, d.firecrawlKey)
	if err != nil {
		return false
	}

	client := newHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}

	var fcResp struct {
		Success bool `json:"success"`
		Data    struct {
			Markdown string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fcResp); err != nil {
		return false
	}

	// Check if content looks like it contains job listings
	content := strings.ToLower(fcResp.Data.Markdown)
	jobIndicators := []string{"developer", "engineer", "remote", "salary", "apply", "hiring", "bounty", "contract"}
	matches := 0
	for _, indicator := range jobIndicators {
		if strings.Contains(content, indicator) {
			matches++
		}
	}
	return matches >= 2
}

func (d *SourceDiscoverer) sourcesPath() string {
	return filepath.Join(d.dataDir, "discovered_sources.jsonl")
}

// ApproveSource updates a discovered source's status to approved.
func ApproveSource(path, url string) error {
	sources, err := ReadDiscoveredSources(path)
	if err != nil {
		return fmt.Errorf("read sources: %w", err)
	}

	found := false
	newSources := make([]DiscoveredSource, 0, len(sources))
	for _, s := range sources {
		if s.URL == url {
			s.Status = "approved"
			found = true
		}
		newSources = append(newSources, s)
	}
	if !found {
		return fmt.Errorf("source %q not found", url)
	}

	// Rewrite the file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	for _, s := range newSources {
		data, err := json.Marshal(s)
		if err != nil {
			continue
		}
		f.Write(append(data, '\n'))
	}
	return f.Sync()
}

// newRequestWithContext creates a POST request with JSON body and auth header.
func newRequestWithContext(ctx context.Context, url string, jsonBody []byte, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return req, nil
}

// newHTTPClient creates an HTTP client with a 30s timeout.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
