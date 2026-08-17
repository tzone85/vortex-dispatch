package llm

import (
	"errors"
	"fmt"
	"strings"
)

// APIError represents a structured error from an LLM provider's HTTP API.
// It carries the HTTP status code and whether the error is transient (retryable).
type APIError struct {
	StatusCode int
	Message    string
	Retryable  bool
	RetryAfter int // seconds; 0 means not specified
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// IsInsufficientBalance returns true when the error indicates the API account
// has run out of credits. This is a fatal condition — retrying won't help.
func IsInsufficientBalance(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	return apiErr.StatusCode == 400 &&
		(strings.Contains(msg, "credit balance") ||
			strings.Contains(msg, "billing") ||
			strings.Contains(msg, "insufficient_quota") ||
			strings.Contains(msg, "out of extra usage"))
}

// IsFatalAPIError returns true when the error is a non-retryable API error
// that will never succeed regardless of retries — e.g. invalid credentials
// (401), insufficient balance (400), or permission denied (403).
func IsFatalAPIError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == 401 || apiErr.StatusCode == 403 || IsInsufficientBalance(err)
}

// IsRateLimited returns true when the error is an HTTP 429 (Too Many Requests).
func IsRateLimited(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == 429
}

// IsOverloaded returns true when the API reports it is overloaded (Anthropic 529).
func IsOverloaded(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == 529
}

// capacitySignatures are substrings that indicate a transient capacity/quota
// exhaustion the caller should back off from rather than treat as a story-quality
// failure. Kept lowercase; match against a lowercased error string.
var capacitySignatures = []string{
	"session limit",        // Claude Max: "You've hit your session limit · resets ..."
	"usage limit",          // alternate Max wording
	"rate limit",           // generic rate limiting
	"rate_limit",           // API error code form
	"too many requests",    // HTTP 429 canonical text
	"overloaded",           // Anthropic 529
	`"api_error_status":429`, // embedded CLI envelope, untyped
	`"api_error_status":529`,
	`"api_error_status": 429`,
	`"api_error_status": 529`,

	// Transient network/transport failures. Like a session limit, these are not
	// a story-quality problem and succeed on retry/resume — so they must take
	// the clean-pause path, not burn the escalation chain. Surfaced by the CLI
	// as e.g. "API Error: The socket connection was closed unexpectedly".
	"socket connection was closed",
	"connection closed unexpectedly",
	"connection reset",
	"connection refused",
	"i/o timeout",
	"tls handshake timeout",
	"unexpected eof",
	"service unavailable",
	"bad gateway",
	"gateway timeout",
}

// ContainsCapacitySignature reports whether a raw string carries a capacity /
// session-limit / overloaded signal. Exported so the CLI client can reuse the
// exact same vocabulary it would otherwise duplicate.
func ContainsCapacitySignature(s string) bool {
	lower := strings.ToLower(s)
	for _, sig := range capacitySignatures {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// capacityNoticeSignatures are the subset of capacity signals that a CLI has been
// observed to leak as *successful completion content* — a session/usage-limit
// banner or the untyped API error envelope. They are high-precision: unlike the
// generic phrases in capacitySignatures ("rate limit", "connection refused",
// "overloaded", …), they essentially never appear as ordinary substrings inside
// legitimate source files.
var capacityNoticeSignatures = []string{
	"hit your session limit", // Claude Max banner leaked as content
	"hit your usage limit",   // alternate Max wording
	`"api_error_status":429`, // untyped CLI error envelope leaked as content
	`"api_error_status":529`,
	`"api_error_status": 429`,
	`"api_error_status": 529`,
}

// leakedNoticeMaxLen bounds how long a leaked capacity/session-limit notice can
// be. Such a notice is a short status blurb; a genuinely resolved source file is
// longer and must not be misread as a capacity error merely because it contains a
// generic phrase like "rate limit" or "connection refused".
const leakedNoticeMaxLen = 512

// LooksLikeLeakedCapacityNotice reports whether an LLM's *successful* response
// content is actually a leaked session-limit / overloaded / rate-limit notice
// rather than real content. Some CLI versions emit such a notice as a successful
// completion instead of an error envelope; written verbatim it would corrupt the
// target (e.g. a conflict-resolved source file).
//
// It fires on (a) the unambiguous session/usage-limit banner or the untyped API
// error envelope at ANY length, or (b) any capacity signature within a SHORT
// blurb. It deliberately does NOT flag a long body that merely contains a generic
// capacity phrase — that is the false positive that would misclassify a correctly
// resolved conflict in rate-limiter / networking / error-handling code as a
// synthetic 429 and wedge the pipeline. Use this (not ContainsCapacitySignature)
// when scanning content that could legitimately BE a large file.
func LooksLikeLeakedCapacityNotice(content string) bool {
	lower := strings.ToLower(content)
	for _, sig := range capacityNoticeSignatures {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	if len(strings.TrimSpace(content)) <= leakedNoticeMaxLen {
		return ContainsCapacitySignature(lower)
	}
	return false
}

// IsCapacityError returns true when the error is a transient capacity exhaustion
// — HTTP 429 (rate/session limit) or 529 (overloaded) — whether it arrived as a
// typed *APIError or as a stringified CLI error that never got classified.
//
// This is distinct from IsFatalAPIError (401/403/billing — permanent): a
// capacity error WILL succeed after the limit resets, so the pipeline should
// pause-and-resume rather than burn the escalation chain or fail the story.
func IsCapacityError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 429 || apiErr.StatusCode == 529 {
			return true
		}
	}
	// Fall back to scanning the error text: the claude CLI path frequently
	// stringifies the session-limit envelope into a plain error before it
	// reaches a decision point.
	return ContainsCapacitySignature(err.Error())
}

// IsRetryable returns true when the error is transient and the request can be retried.
func IsRetryable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Retryable
}

// RetryAfterSeconds returns the Retry-After hint from the API, or 0 if not set.
func RetryAfterSeconds(err error) int {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0
	}
	return apiErr.RetryAfter
}
