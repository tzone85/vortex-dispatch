package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStrictSameOrigin_RejectsCrossPortLocalhost pins the cross-port
// CSRF defence. Prior `OriginPatterns: []string{"localhost:*",
// "127.0.0.1:*"}` accepted any localhost port — so a page at
// http://localhost:3000 could open ws://localhost:8080/ws with
// credentials:'include', and the SameSite=Strict cookie would still
// ship because `localhost` is a single registrable site across ports.
// The fix requires Origin host to equal r.Host (the dashboard's own
// listener); cross-port localhost is now rejected.
func TestStrictSameOrigin_RejectsCrossPortLocalhost(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		want   bool // true = should pass (allow)
	}{
		{"same host:port", "http://localhost:8080", "localhost:8080", true},
		{"127 same port", "http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"empty origin (curl/native)", "", "localhost:8080", true},
		{"cross-port localhost (the bug)", "http://localhost:3000", "localhost:8080", false},
		{"cross-host loopback", "http://127.0.0.1:8080", "localhost:8080", false},
		{"foreign host", "http://evil.example.com", "localhost:8080", false},
		{"scheme switch but same host", "https://localhost:8080", "localhost:8080", true},
		{"empty host on origin", "http://", "localhost:8080", false},
		{"malformed origin", "://garbage", "localhost:8080", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/ws", nil)
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			err := strictSameOrigin(r)
			if (err == nil) != c.want {
				t.Errorf("strictSameOrigin(origin=%q, host=%q) err=%v, want pass=%v", c.origin, c.host, err, c.want)
			}
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "origin") {
				t.Errorf("error message should mention origin: %v", err)
			}
		})
	}
}

// TestStrictSameOriginWS_Exported pins the exported alias used by the
// memory dashboard package — sibling packages cannot reach the
// lowercase strictSameOrigin, and a regression that removed the
// alias would silently re-open the cross-port CSRF on the memory
// surface.
func TestStrictSameOriginWS_Exported(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Host = "localhost:8080"
	r.Header.Set("Origin", "http://localhost:3000")
	if err := StrictSameOriginWS(r); err == nil {
		t.Fatal("expected cross-port rejection via exported helper")
	}
}
