package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestToolCallAdapter_ImplementsClientInterface(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "ok"})
	var _ llm.Client = llm.NewToolCallAdapter(inner, nil)
}

func TestToolCallAdapter_PassthroughForNonGemmaModel(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "response"})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleTechLead))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "claude-sonnet-4-6-20250620",
		System: "You are a tech lead",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Plan this"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "response" {
		t.Errorf("expected 'response', got %q", resp.Content)
	}

	recorded := inner.CallAt(0)
	if strings.Contains(recorded.System, "<tools>") {
		t.Error("system prompt should not contain tool schema for non-Gemma model")
	}
}

func TestToolCallAdapter_InjectsSchemaForGemmaModel(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: `{"on_track":true,"concerns":[],"reprioritize":[]}`})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	_, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "gemma-4-27b-it",
		System: "You are a Supervisor",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Review progress"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	recorded := inner.CallAt(0)
	if !strings.Contains(recorded.System, "<tools>") {
		t.Error("system prompt should contain <tools> for Gemma model")
	}
	if !strings.Contains(recorded.System, "report_status") {
		t.Error("system prompt should contain tool name 'report_status'")
	}
	if !strings.HasPrefix(recorded.System, "You are a Supervisor") {
		t.Error("original system prompt should be preserved as prefix")
	}
}

func TestToolCallAdapter_ParsesToolCallTokens(t *testing.T) {
	toolCallResponse := "<|tool_call|>\nreport_status\n{\"on_track\":true,\"concerns\":[\"slow progress\"],\"reprioritize\":[]}\n<|end_tool_call|>"

	inner := llm.NewReplayClient(llm.CompletionResponse{Content: toolCallResponse})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(resp.Content, `"on_track":true`) {
		t.Errorf("expected parsed JSON in content, got %q", resp.Content)
	}
	if strings.Contains(resp.Content, "<|tool_call|>") {
		t.Error("content should not contain raw tool-call tokens")
	}
}

func TestToolCallAdapter_GracefulDegradationFreeTextJSON(t *testing.T) {
	freeTextJSON := `{"on_track":false,"concerns":["drift detected"],"reprioritize":["s-003"]}`
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: freeTextJSON})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != freeTextJSON {
		t.Errorf("expected passthrough of free-text JSON, got %q", resp.Content)
	}
}

func TestToolCallAdapter_NilSchemaPassthrough(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "ok"})
	adapter := llm.NewToolCallAdapter(inner, nil)

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "gemma-4-27b-it",
		System: "Original prompt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}

	recorded := inner.CallAt(0)
	if recorded.System != "Original prompt" {
		t.Errorf("expected unmodified system prompt, got %q", recorded.System)
	}
}
