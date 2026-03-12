package llm

import (
	"context"
	"fmt"
	"log"
	"time"
)

// RetryClient wraps an LLM Client with automatic retry for transient errors
// (rate limits, overloaded, server errors). Non-retryable errors such as
// insufficient balance are returned immediately.
type RetryClient struct {
	inner      Client
	maxRetries int
	baseDelay  time.Duration
}

// NewRetryClient wraps inner with retry logic. maxRetries is the number of
// additional attempts after the first failure (so total attempts = 1 + maxRetries).
func NewRetryClient(inner Client, maxRetries int) *RetryClient {
	return &RetryClient{
		inner:      inner,
		maxRetries: maxRetries,
		baseDelay:  2 * time.Second,
	}
}

// Complete delegates to the inner client with retry for transient errors.
func (r *RetryClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Non-retryable errors (e.g. billing) fail fast.
		if !IsRetryable(err) {
			return CompletionResponse{}, err
		}

		// Exponential backoff, but prefer Retry-After header if set.
		delay := r.baseDelay * time.Duration(1<<uint(attempt))
		if ra := RetryAfterSeconds(err); ra > 0 {
			delay = time.Duration(ra) * time.Second
		}

		log.Printf("[llm] retryable error (attempt %d/%d), waiting %s: %v",
			attempt+1, r.maxRetries+1, delay, err)

		select {
		case <-ctx.Done():
			return CompletionResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}

	return CompletionResponse{}, fmt.Errorf("max retries (%d) exceeded: %w", r.maxRetries, lastErr)
}
