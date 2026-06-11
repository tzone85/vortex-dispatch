package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TokenCookieName is the cookie set on the first authenticated request so
// the browser session carries the token without exposing it in every URL.
const TokenCookieName = "vxd_dashboard_token"

// NonceQueryParam is the URL query parameter that carries a SINGLE-USE
// bootstrap nonce. The server validates the nonce once, invalidates it,
// and sets the persistent token cookie. The persistent token itself
// never travels in a URL — keeps it out of browser history, shoulder-
// surfing, and any leaked Referer headers.
const NonceQueryParam = "bootstrap"

// authBypassedPaths are unauthenticated endpoints — typically liveness
// probes. Anything else is rejected without a valid token.
var authBypassedPaths = map[string]struct{}{
	"/health": {},
}

// LoadOrGenerateToken returns the dashboard auth token. Precedence:
//
//  1. VXD_DASHBOARD_TOKEN env var — operator override.
//  2. Existing token file (mode 0o600).
//  3. Newly generated 32-byte hex token, written to the file with 0o600.
//
// An empty path skips the file fallback (used by tests).
func LoadOrGenerateToken(path string) (string, error) {
	if t := strings.TrimSpace(os.Getenv("VXD_DASHBOARD_TOKEN")); t != "" {
		return t, nil
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			tok := strings.TrimSpace(string(data))
			if tok != "" {
				return tok, nil
			}
		}
	}
	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("create token dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
			return "", fmt.Errorf("write token file: %w", err)
		}
	}
	return tok, nil
}

func generateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// GenerateBootstrapNonce returns a 32-byte crypto/rand hex string suitable
// for AuthOptions.BootstrapNonce. Exported so other packages (memory
// dashboard) can build a one-shot URL bootstrap without re-implementing.
func GenerateBootstrapNonce() (string, error) { return generateToken() }

// AuthOptions configures NewAuthMiddleware.
type AuthOptions struct {
	// Token is the persistent bearer token. Required unless
	// AllowUnauthenticated is true. NewAuthMiddleware PANICS if both are
	// unset — silent fail-open auth is a documented anti-pattern.
	Token string

	// BootstrapNonce, if set, is a single-use 32-byte hex string the
	// server accepts via ?bootstrap=X to set the cookie. Used for the
	// auto-opened browser flow so the persistent token never appears in
	// any URL. Invalidated on first successful use.
	BootstrapNonce string

	// AllowUnauthenticated is the EXPLICIT escape hatch. Set true only
	// when a caller deliberately wants no auth (test harnesses or "I
	// locked myself out of the dashboard" diagnostics). The middleware
	// short-circuits to the inner handler in that case.
	AllowUnauthenticated bool
}

// authenticator binds token + single-use nonce together and serializes
// nonce consumption.
type authenticator struct {
	token         string
	tokenBytes    []byte
	nonce         string
	nonceMu       sync.Mutex
	nonceConsumed bool
}

// NewAuthMiddleware returns middleware that enforces opts.Token on every
// request except authBypassedPaths. Token is accepted via, in priority:
//
//  1. Authorization: Bearer <token>
//  2. ?bootstrap=<single-use-nonce> — sets the cookie and is invalidated
//     so the same URL cannot replay.
//  3. The TokenCookieName cookie (set on a successful bootstrap).
//
// Constant-time comparison avoids leaking timing info.
//
// Panics when opts.Token is empty AND opts.AllowUnauthenticated is false.
// Silent fail-open is treated as misconfiguration, not a feature.
func NewAuthMiddleware(opts AuthOptions) func(http.Handler) http.Handler {
	if opts.Token == "" && !opts.AllowUnauthenticated {
		panic("web.NewAuthMiddleware: empty Token requires explicit AllowUnauthenticated=true; refusing to start an unauthenticated dashboard")
	}
	if opts.AllowUnauthenticated {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	a := &authenticator{
		token:      opts.Token,
		tokenBytes: []byte(opts.Token),
		nonce:      opts.BootstrapNonce,
	}
	return a.wrap
}

// RequireToken is a thin compatibility wrapper for callers that don't
// need the bootstrap-nonce flow. Equivalent to NewAuthMiddleware with
// only Token set (or AllowUnauthenticated when token is empty — only
// the AllowUnauthenticated path is explicit; empty token still panics
// in NewAuthMiddleware, mirroring the safety guarantee).
func RequireToken(token string, next http.Handler) http.Handler {
	mw := NewAuthMiddleware(AuthOptions{
		Token:                token,
		AllowUnauthenticated: token == "",
	})
	return mw(next)
}

func (a *authenticator) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// `Referrer-Policy: no-referrer` keeps any URL noise (including
		// the one-time bootstrap nonce on first load) out of outbound
		// Referer headers. Defence-in-depth; the nonce is single-use
		// anyway.
		w.Header().Set("Referrer-Policy", "no-referrer")

		if _, ok := authBypassedPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Authorization header.
		if header := r.Header.Get("Authorization"); header != "" {
			if strings.HasPrefix(header, "Bearer ") {
				got := []byte(strings.TrimPrefix(header, "Bearer "))
				if subtle.ConstantTimeCompare(got, a.tokenBytes) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// 2. Single-use bootstrap nonce. Sets the cookie + invalidates
		// the nonce so the URL can't replay.
		if q := r.URL.Query().Get(NonceQueryParam); q != "" {
			if a.tryConsumeNonce(q) {
				http.SetCookie(w, &http.Cookie{
					Name:     TokenCookieName,
					Value:    a.token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					// Secure: cookie travels over TLS only. The dashboard
					// binds to localhost today (plain HTTP), but operators
					// often front it with an HTTPS reverse proxy. Without
					// Secure, the browser would happily send the cookie
					// over BOTH the HTTPS proxy AND any plain HTTP
					// connection — leaking the session token if the
					// operator mistakenly browses the HTTP address.
					Secure: true,
				})
				next.ServeHTTP(w, r)
				return
			}
		}

		// 3. Cookie set by a previous successful bootstrap.
		if c, err := r.Cookie(TokenCookieName); err == nil && c.Value != "" {
			if subtle.ConstantTimeCompare([]byte(c.Value), a.tokenBytes) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="vxd-dashboard"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// tryConsumeNonce returns true iff the supplied nonce matches AND has
// not been consumed. On a successful match the stored nonce is cleared
// so subsequent loads of the same URL fail.
func (a *authenticator) tryConsumeNonce(provided string) bool {
	a.nonceMu.Lock()
	defer a.nonceMu.Unlock()
	if a.nonceConsumed || a.nonce == "" || provided == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(a.nonce)) != 1 {
		return false
	}
	a.nonceConsumed = true
	a.nonce = ""
	return true
}
