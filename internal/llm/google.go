package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const googleAIBaseURL = "https://generativelanguage.googleapis.com"

// GoogleAIClient communicates with the Google AI Studio generateContent API.
type GoogleAIClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewGoogleAIClient creates a client configured with the given API key.
func NewGoogleAIClient(apiKey string) *GoogleAIClient {
	return &GoogleAIClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    googleAIBaseURL,
	}
}

// WithBaseURL returns a copy of the client with a custom base URL,
// useful for testing with httptest servers.
func (c *GoogleAIClient) WithBaseURL(url string) *GoogleAIClient {
	return &GoogleAIClient{
		apiKey:     c.apiKey,
		httpClient: c.httpClient,
		baseURL:    url,
	}
}

type googleRequest struct {
	SystemInstruction *googleSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []googleContent          `json:"contents"`
	GenerationConfig  googleGenerationConfig   `json:"generationConfig"`
}

type googleSystemInstruction struct {
	Parts []googlePart `json:"parts"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature,omitempty"`
}

type googleResponse struct {
	Candidates    []googleCandidate   `json:"candidates"`
	ModelVersion  string              `json:"modelVersion"`
	UsageMetadata googleUsageMetadata `json:"usageMetadata"`
}

type googleCandidate struct {
	Content      googleContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type googleUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

// Complete sends a completion request to the Google AI Studio generateContent
// API and returns the parsed response.
func (c *GoogleAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	contents := make([]googleContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := string(m.Role)
		if role == string(RoleAssistant) {
			role = "model"
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googlePart{{Text: m.Content}},
		})
	}

	body := googleRequest{
		Contents: contents,
		GenerationConfig: googleGenerationConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}

	if req.System != "" {
		body.SystemInstruction = &googleSystemInstruction{
			Parts: []googlePart{{Text: req.System}},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := limitedReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, c.classifyError(resp.StatusCode, respBody, resp.Header)
	}

	var apiResp googleResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(apiResp.Candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("google AI returned no candidates")
	}

	candidate := apiResp.Candidates[0]
	content := ""
	for _, part := range candidate.Content.Parts {
		content += part.Text
	}

	return CompletionResponse{
		Content:    content,
		Model:      apiResp.ModelVersion,
		StopReason: candidate.FinishReason,
		Usage: Usage{
			InputTokens:  apiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: apiResp.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

// classifyError maps Google AI error responses to APIError. Notably,
// RESOURCE_EXHAUSTED (HTTP 400) is remapped to StatusCode 429 so that
// the existing IsRateLimited() helper catches it for fallback triggering.
func (c *GoogleAIClient) classifyError(statusCode int, body []byte, headers http.Header) *APIError {
	msg := string(body)
	retryAfter, _ := strconv.Atoi(headers.Get("Retry-After"))

	if statusCode == 400 && strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		return &APIError{
			StatusCode: 429,
			Message:    msg,
			Retryable:  true,
			RetryAfter: retryAfter,
		}
	}

	retryable := statusCode == 429 || statusCode >= 500
	return &APIError{
		StatusCode: statusCode,
		Message:    msg,
		Retryable:  retryable,
		RetryAfter: retryAfter,
	}
}
