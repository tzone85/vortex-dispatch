package improve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/improve"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestAnalyzer_TriageScoresFindings(t *testing.T) {
	triageResponse := `{"relevance": 8, "impact": 7, "risk": 3, "effort": "S", "category": "performance", "reasoning": "Directly applicable"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": triageResponse}}, "role": "model"}, "finishReason": "STOP"},
			},
			"modelVersion": "gemma-4-27b-it",
			"usageMetadata": map[string]any{"promptTokenCount": 100, "candidatesTokenCount": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	googleClient := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	analyzer := improve.NewAnalyzer(googleClient, "", 5)

	findings := []improve.Finding{
		{Title: "Go 1.24 iterators", Content: "New stdlib iterators", Category: "go_ecosystem"},
	}

	scored, err := analyzer.Triage(context.Background(), findings)
	if err != nil {
		t.Fatalf("triage: %v", err)
	}
	if len(scored) != 1 {
		t.Fatalf("expected 1 scored finding, got %d", len(scored))
	}
	if scored[0].Relevance != 8 {
		t.Errorf("expected relevance 8, got %d", scored[0].Relevance)
	}
	if scored[0].Impact != 7 {
		t.Errorf("expected impact 7, got %d", scored[0].Impact)
	}
}

func TestAnalyzer_FiltersBelowThreshold(t *testing.T) {
	lowScoreResponse := `{"relevance": 2, "impact": 3, "risk": 1, "effort": "S", "category": "general", "reasoning": "Not relevant"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": lowScoreResponse}}, "role": "model"}, "finishReason": "STOP"},
			},
			"modelVersion": "gemma-4-27b-it",
			"usageMetadata": map[string]any{"promptTokenCount": 100, "candidatesTokenCount": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	googleClient := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	analyzer := improve.NewAnalyzer(googleClient, "", 5)

	findings := []improve.Finding{
		{Title: "Irrelevant", Content: "Not useful", Category: "general_se"},
	}

	scored, _ := analyzer.Triage(context.Background(), findings)
	if len(scored) != 0 {
		t.Fatalf("expected 0 findings after filtering, got %d", len(scored))
	}
}

func TestRankFindings_SortsCorrectly(t *testing.T) {
	a := improve.ScoredFinding{Relevance: 8, Impact: 9, Risk: 2, Rank: 24}
	b := improve.ScoredFinding{Relevance: 6, Impact: 5, Risk: 1, Rank: 15}

	ranked := improve.RankFindings([]improve.ScoredFinding{b, a})
	if ranked[0].Rank != 24 {
		t.Error("expected higher-ranked finding first")
	}
}
