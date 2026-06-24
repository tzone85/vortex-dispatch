package llm

import (
	"fmt"
	"testing"
)

// classifyCLIError must turn a session-limit / overloaded CLI envelope into a
// typed *APIError so downstream IsCapacityError / IsRateLimited see it. Before
// the fix it fell through to a plain "claude CLI error" that no classifier could
// recognise, cascading the 429 through the whole escalation chain.
func TestClassifyCLIError_SessionLimit(t *testing.T) {
	sessionLimitJSON := `{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"result":"You've hit your session limit · resets 12:20pm (Africa/Johannesburg)"}`

	cases := []struct {
		name    string
		output  string
		wantCap bool
		wantFat bool
	}{
		{"session limit envelope", sessionLimitJSON, true, false},
		{"rate limit text", "rate limit exceeded", true, false},
		{"too many requests", "Error: too many requests", true, false},
		{"overloaded", "service overloaded, try again", true, false},
		{"billing stays fatal", "your credit balance is too low", false, true},
		{"auth stays fatal", "authentication failed", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyCLIError(fmt.Errorf("exit status 1"), []byte(tc.output))
			if got := IsCapacityError(err); got != tc.wantCap {
				t.Errorf("IsCapacityError = %v, want %v (err=%v)", got, tc.wantCap, err)
			}
			if got := IsFatalAPIError(err); got != tc.wantFat {
				t.Errorf("IsFatalAPIError = %v, want %v (err=%v)", got, tc.wantFat, err)
			}
		})
	}
}
