package improve

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// ScoredFinding is a Finding with triage scores from Gemma 4.
type ScoredFinding struct {
	Finding
	Relevance int    `json:"relevance"`
	Impact    int    `json:"impact"`
	Risk      int    `json:"risk"`
	Effort    string `json:"effort"`
	Reasoning string `json:"reasoning"`
	Rank      int    `json:"rank"`
}

// AnalyzedFinding is a ScoredFinding with deep analysis from Claude.
type AnalyzedFinding struct {
	ScoredFinding
	ImplementationPlan string `json:"implementation_plan"`
	SecurityReview     string `json:"security_review"`
	LicenseCheck       string `json:"license_check"`
	TestStrategy       string `json:"test_strategy"`
	GoNoGo             string `json:"go_no_go"`
}

// Analyzer performs two-stage analysis: Gemma 4 triage + Claude deep analysis.
type Analyzer struct {
	triageClient llm.Client
	claudePath   string
	threshold    int
}

// NewAnalyzer creates an analyzer with the given Gemma 4 client and Claude CLI path.
func NewAnalyzer(triageClient llm.Client, claudePath string, threshold int) *Analyzer {
	return &Analyzer{
		triageClient: triageClient,
		claudePath:   claudePath,
		threshold:    threshold,
	}
}

type triageResponse struct {
	Relevance int    `json:"relevance"`
	Impact    int    `json:"impact"`
	Risk      int    `json:"risk"`
	Effort    string `json:"effort"`
	Category  string `json:"category"`
	Reasoning string `json:"reasoning"`
}

// Triage scores findings via Gemma 4 and filters below the relevance threshold.
func (a *Analyzer) Triage(ctx context.Context, findings []Finding) ([]ScoredFinding, error) {
	var scored []ScoredFinding

	for i, f := range findings {
		log.Printf("  [%d/%d] Scoring %q ...", i+1, len(findings), f.Title)
		prompt := fmt.Sprintf(`Score this finding for relevance to VXD (an AI agent orchestration CLI tool written in Go):

Title: %s
Source: %s
Category: %s
Content: %s

Respond with JSON only:
{"relevance": 0-10, "impact": 0-10, "risk": 0-10, "effort": "S|M|L", "category": "security|performance|feature|dependency|docs|architecture", "reasoning": "why"}`, f.Title, f.SourceURL, f.Category, f.Content)

		resp, err := a.triageClient.Complete(ctx, llm.CompletionRequest{
			Model:     "gemma-4-26b-a4b-it",
			MaxTokens: 500,
			System:    "You are a technical analyst scoring research findings for an AI agent orchestration tool called VXD. Respond with JSON only.",
			Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
		})
		if err != nil {
			log.Printf("[analyzer] triage failed for %q: %v", f.Title, err)
			continue
		}

		var tr triageResponse
		cleaned := strings.TrimSpace(resp.Content)
		if idx := strings.Index(cleaned, "{"); idx >= 0 {
			cleaned = cleaned[idx:]
		}
		if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
			cleaned = cleaned[:idx+1]
		}
		if err := json.Unmarshal([]byte(cleaned), &tr); err != nil {
			log.Printf("[analyzer] parse triage for %q: %v", f.Title, err)
			continue
		}

		if tr.Relevance < a.threshold {
			log.Printf("  [%d/%d] %q → relevance %d (below threshold %d, skipped)", i+1, len(findings), f.Title, tr.Relevance, a.threshold)
			continue
		}
		log.Printf("  [%d/%d] %q → relevance=%d impact=%d risk=%d rank=%d", i+1, len(findings), f.Title, tr.Relevance, tr.Impact, tr.Risk, (tr.Impact*2)+tr.Relevance-tr.Risk)

		rank := (tr.Impact * 2) + tr.Relevance - tr.Risk
		scored = append(scored, ScoredFinding{
			Finding:   f,
			Relevance: tr.Relevance,
			Impact:    tr.Impact,
			Risk:      tr.Risk,
			Effort:    tr.Effort,
			Reasoning: tr.Reasoning,
			Rank:      rank,
		})
	}

	return RankFindings(scored), nil
}

// RankFindings sorts scored findings by rank descending.
func RankFindings(findings []ScoredFinding) []ScoredFinding {
	sorted := make([]ScoredFinding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Rank > sorted[j].Rank
	})
	return sorted
}
