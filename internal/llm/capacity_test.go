package llm_test

import (
	"fmt"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestIsCapacityError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		// Typed APIError status codes.
		{name: "429 typed", err: &llm.APIError{StatusCode: 429, Message: "rate limited"}, expect: true},
		{name: "529 overloaded typed", err: &llm.APIError{StatusCode: 529, Message: "overloaded"}, expect: true},
		{name: "wrapped 429 typed", err: fmt.Errorf("manager: %w", &llm.APIError{StatusCode: 429, Message: "x"}), expect: true},

		// Stringified CLI errors that never became typed APIErrors — the real
		// production failure: the claude CLI emits a JSON envelope whose result
		// is "You've hit your session limit" and vxd stringifies it.
		{name: "session limit string", err: fmt.Errorf(`claude CLI error: exit status 1 (output: {"is_error":true,"api_error_status":429,"result":"You've hit your session limit · resets 12:20pm (Africa/Johannesburg)"})`), expect: true},
		{name: "usage limit string", err: fmt.Errorf("claude CLI returned error: usage limit reached"), expect: true},
		{name: "rate limit string", err: fmt.Errorf("claude CLI error: rate limit exceeded"), expect: true},
		{name: "too many requests string", err: fmt.Errorf("too many requests"), expect: true},
		{name: "overloaded string", err: fmt.Errorf("the service is currently overloaded"), expect: true},
		{name: "api_error_status 429 embedded", err: fmt.Errorf(`output: {"api_error_status":429}`), expect: true},
		{name: "api_error_status 529 embedded", err: fmt.Errorf(`output: {"api_error_status":529}`), expect: true},

		// Must NOT classify as capacity — these are fatal or ordinary failures.
		{name: "401 auth", err: &llm.APIError{StatusCode: 401, Message: "unauthorized"}, expect: false},
		{name: "400 billing", err: &llm.APIError{StatusCode: 400, Message: "credit balance too low"}, expect: false},
		{name: "ordinary compile error", err: fmt.Errorf("undefined: Foo"), expect: false},
		{name: "nil", err: nil, expect: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := llm.IsCapacityError(tc.err); got != tc.expect {
				t.Errorf("IsCapacityError(%v) = %v, want %v", tc.err, got, tc.expect)
			}
		})
	}
}

// A capacity error must never be misclassified as fatal — they take different
// pipeline paths (fatal = give up; capacity = pause-and-resume-after-reset).
func TestCapacityErrorIsNotFatal(t *testing.T) {
	sessionLimit := fmt.Errorf(`claude CLI error: exit status 1 (output: {"is_error":true,"api_error_status":429,"result":"You've hit your session limit"})`)
	if llm.IsFatalAPIError(sessionLimit) {
		t.Error("session-limit must not be classified as fatal")
	}
	if !llm.IsCapacityError(sessionLimit) {
		t.Error("session-limit must be classified as capacity")
	}
}
