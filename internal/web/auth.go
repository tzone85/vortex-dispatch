package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// tokenCookieName is the cookie the dashboard uses to carry the session token
// across reloads once the operator has opened the authenticated URL.
const tokenCookieName = "vxd_token"

// newAuthToken returns a cryptographically random session token. The dashboard
// binds to localhost, but localhost is shared by every process on the host, so
// a per-session secret is required to stop unrelated local processes (which
// present no browser Origin) from driving the mutating WebSocket command
// channel. The token is delivered out-of-band: it is printed to the operator's
// terminal and embedded in the URL the server opens in their browser.
func newAuthToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is effectively impossible; degrade loudly rather
		// than silently shipping a predictable token.
		log.Printf("[web] WARNING: crypto/rand failed (%v); using degraded token source", err)
		return hex.EncodeToString([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())))
	}
	return hex.EncodeToString(b)
}

// requestToken extracts a candidate token from the request: the `token` query
// parameter (used on the first, server-opened load) or the session cookie
// (used on subsequent reloads that drop the query string).
func requestToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if c, err := r.Cookie(tokenCookieName); err == nil {
		return c.Value
	}
	return ""
}

// validToken reports whether the request carries the correct session token.
// The comparison is constant-time to avoid leaking the token via timing.
func (s *Server) validToken(r *http.Request) bool {
	if s.authToken == "" {
		// No token configured — auth disabled (used by tests that construct a
		// Server directly). Production always sets one in NewServer.
		return true
	}
	got := requestToken(r)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.authToken)) == 1
}
