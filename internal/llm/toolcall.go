package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCallAdapter wraps a Client to inject tool schemas into system prompts
// for Gemma models and parse tool-call tokens from responses. For non-Gemma
// models or when schema is nil, requests pass through unchanged.
type ToolCallAdapter struct {
	inner      Client
	toolSchema *ToolSchema
}

// NewToolCallAdapter creates an adapter. If schema is nil, all requests
// pass through unchanged regardless of model.
func NewToolCallAdapter(inner Client, schema *ToolSchema) *ToolCallAdapter {
	return &ToolCallAdapter{
		inner:      inner,
		toolSchema: schema,
	}
}

// Complete augments the request with tool schemas for Gemma models, then
// parses tool-call tokens from the response. Non-Gemma models pass through.
func (a *ToolCallAdapter) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if !a.shouldInjectTools(req.Model) {
		return a.inner.Complete(ctx, req)
	}

	augmented := a.augmentRequest(req)
	resp, err := a.inner.Complete(ctx, augmented)
	if err != nil {
		return resp, err
	}

	resp.Content = a.parseToolCallResponse(resp.Content)
	return resp, nil
}

// shouldInjectTools returns true if the model is a Gemma variant and a
// schema is configured.
func (a *ToolCallAdapter) shouldInjectTools(model string) bool {
	return a.toolSchema != nil && strings.Contains(strings.ToLower(model), "gemma")
}

// augmentRequest appends tool definitions to the system prompt.
func (a *ToolCallAdapter) augmentRequest(req CompletionRequest) CompletionRequest {
	schemaJSON, err := json.Marshal([]ToolSchema{*a.toolSchema})
	if err != nil {
		return req
	}

	toolBlock := fmt.Sprintf("\n\nAvailable tools:\n<tools>\n%s\n</tools>\n\nWhen calling a tool, use this format:\n<|tool_call|>\ntool_name\n{json_arguments}\n<|end_tool_call|>", string(schemaJSON))

	return CompletionRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System + toolBlock,
	}
}

// parseToolCallResponse extracts JSON from tool-call tokens if present.
// If no tool-call tokens are found, returns the content unchanged for
// graceful degradation to free-text JSON parsing.
func (a *ToolCallAdapter) parseToolCallResponse(content string) string {
	startToken := "<|tool_call|>"
	endToken := "<|end_tool_call|>"

	startIdx := strings.Index(content, startToken)
	if startIdx == -1 {
		return content
	}

	endIdx := strings.Index(content, endToken)
	if endIdx == -1 {
		return content
	}

	inner := content[startIdx+len(startToken) : endIdx]
	inner = strings.TrimSpace(inner)

	// The format is: tool_name\n{json}
	// Find the first { or [ to locate the JSON payload
	jsonStart := strings.IndexAny(inner, "{[")
	if jsonStart == -1 {
		return content
	}

	return strings.TrimSpace(inner[jsonStart:])
}
