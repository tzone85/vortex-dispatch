package llm

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	defaultRPM = 10
	defaultRPD = 1500
)

// QuotaTracker tracks request counts against rate limits using simple
// counters protected by a mutex. Counters reset on minute/day boundaries
// without background goroutines.
type QuotaTracker struct {
	mu sync.Mutex

	rpmLimit int
	rpdLimit int

	minuteCount int
	dayCount    int

	currentMinute int
	currentDay    int

	exhaustedUntil time.Time
}

// NewQuotaTracker creates a tracker with the given limits.
func NewQuotaTracker(rpmLimit, rpdLimit int) *QuotaTracker {
	now := time.Now()
	return &QuotaTracker{
		rpmLimit:      rpmLimit,
		rpdLimit:      rpdLimit,
		currentMinute: now.Minute(),
		currentDay:    now.YearDay(),
	}
}

// IsExhausted returns true if either counter is at 90% of its limit
// or if a manual exhaustion cooldown is active.
func (q *QuotaTracker) IsExhausted() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.maybeReset()

	if time.Now().Before(q.exhaustedUntil) {
		return true
	}

	return q.minuteCount >= (q.rpmLimit*9)/10 || q.dayCount >= (q.rpdLimit*9)/10
}

// RecordRequest increments the request counters.
func (q *QuotaTracker) RecordRequest() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.maybeReset()
	q.minuteCount++
	q.dayCount++
}

// MarkExhausted suppresses primary usage for the given cooldown.
func (q *QuotaTracker) MarkExhausted(cooldown time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.exhaustedUntil = time.Now().Add(cooldown)
}

// maybeReset checks if minute/day boundaries have been crossed and
// resets counters accordingly. Must be called under lock.
func (q *QuotaTracker) maybeReset() {
	now := time.Now()
	if now.Minute() != q.currentMinute {
		q.minuteCount = 0
		q.currentMinute = now.Minute()
	}
	if now.YearDay() != q.currentDay {
		q.dayCount = 0
		q.currentDay = now.YearDay()
	}
}

// FallbackClient wraps a primary Client and falls back to a secondary
// when the primary fails due to quota exhaustion or other errors.
type FallbackClient struct {
	primary      Client
	fallback     Client
	quotaTracker *QuotaTracker
}

// NewFallbackClient creates a FallbackClient with default Google AI free-tier limits.
func NewFallbackClient(primary, fallback Client) *FallbackClient {
	return &FallbackClient{
		primary:      primary,
		fallback:     fallback,
		quotaTracker: NewQuotaTracker(defaultRPM, defaultRPD),
	}
}

// NewFallbackClientWithLimits creates a FallbackClient with custom rate limits,
// useful for testing.
func NewFallbackClientWithLimits(primary, fallback Client, rpmLimit, rpdLimit int) *FallbackClient {
	return &FallbackClient{
		primary:      primary,
		fallback:     fallback,
		quotaTracker: NewQuotaTracker(rpmLimit, rpdLimit),
	}
}

// Complete tries the primary client first. On quota/rate-limit errors or any
// failure, it falls back to the secondary client. If the quota tracker
// indicates the primary is exhausted, it skips directly to fallback.
func (f *FallbackClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if !f.quotaTracker.IsExhausted() {
		resp, err := f.primary.Complete(ctx, req)
		if err == nil {
			f.quotaTracker.RecordRequest()
			return resp, nil
		}

		if IsRateLimited(err) {
			cooldown := 60 * time.Second
			if ra := RetryAfterSeconds(err); ra > 0 {
				cooldown = time.Duration(ra) * time.Second
			}
			f.quotaTracker.MarkExhausted(cooldown)
			log.Printf("[fallback] primary rate limited, switching to fallback for %s", cooldown)
		} else if IsFatalAPIError(err) {
			log.Printf("[fallback] primary fatal error (misconfiguration?): %v", err)
		} else {
			log.Printf("[fallback] primary error, trying fallback: %v", err)
		}
	} else {
		log.Printf("[fallback] primary quota exhausted, using fallback directly")
	}

	return f.fallback.Complete(ctx, req)
}
