package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDryRunClient_PlanningResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System:   "You are the Tech Lead of VXD...",
		Messages: []Message{{Role: RoleUser, Content: "Decompose this requirement: Add REST API"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "s-001") {
		t.Error("planning response should contain story IDs")
	}
	if !strings.Contains(resp.Content, "scaffold") {
		t.Error("planning response should contain scaffold story")
	}

	// Verify response is valid JSON (planner parses it)
	var stories []map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &stories); err != nil {
		t.Errorf("planning response should be valid JSON: %v", err)
	}
	if len(stories) != 3 {
		t.Errorf("planning response should contain 3 stories, got %d", len(stories))
	}
}

func TestDryRunClient_ReviewResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System: "You are the QA Agent for Team VXD. Review...",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "passed") {
		t.Error("review response should contain passed field")
	}
	if !strings.Contains(resp.Content, "true") {
		t.Error("review response should have passed=true")
	}
}

func TestDryRunClient_ManagerResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System: "You are the Manager. Diagnose the issue...",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "diagnosis") {
		t.Error("manager response should contain diagnosis field")
	}
	if !strings.Contains(resp.Content, "retry") {
		t.Error("manager response should contain retry action")
	}
}

func TestDryRunClient_SupervisorResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System: "You are the Supervisor. Check progress...",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "ASSESSMENT") {
		t.Error("supervisor response should contain ASSESSMENT")
	}
}

func TestDryRunClient_DeepAnalysisResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System: "You are the Architect. Analyse the codebase...",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "PROJECT PURPOSE") {
		t.Error("deep analysis response should contain PROJECT PURPOSE")
	}
}

func TestDryRunClient_DefaultResponse(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		System:   "You are a generic assistant.",
		Messages: []Message{{Role: RoleUser, Content: "Hello world"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "[DRY RUN]") {
		t.Error("default response should contain [DRY RUN] prefix")
	}
}

func TestDryRunClient_Delay(t *testing.T) {
	client := NewDryRunClient(50 * time.Millisecond)
	start := time.Now()
	_, err := client.Complete(context.Background(), CompletionRequest{
		System: "test",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected at least 40ms delay, got %s", elapsed)
	}
}

func TestDryRunClient_CallCount(t *testing.T) {
	client := NewDryRunClient(0)
	client.Complete(context.Background(), CompletionRequest{System: "test1"})
	client.Complete(context.Background(), CompletionRequest{System: "test2"})
	if client.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2", client.CallCount())
	}
}

func TestDryRunClient_ContextCancellation(t *testing.T) {
	client := NewDryRunClient(5 * time.Second) // long delay
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Complete(ctx, CompletionRequest{System: "test"})
	if err == nil {
		t.Error("should return error on cancelled context")
	}
}

func TestDryRunClient_ModelPassthrough(t *testing.T) {
	client := NewDryRunClient(0)
	resp, err := client.Complete(context.Background(), CompletionRequest{
		Model:  "claude-sonnet-4-6",
		System: "test",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-6")
	}
}

func TestDryRunClient_SatisfiesClientInterface(t *testing.T) {
	// Compile-time check that DryRunClient implements Client
	var _ Client = (*DryRunClient)(nil)
}
