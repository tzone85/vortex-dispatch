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
	mw, _ := NewAuthMiddlewareWithRotator(opts)
	return mw
}

// NonceRotator rotates the single-use bootstrap nonce. Daemons reused across
// many `vxd req` invocations call Rotate before printing a fresh URL so each
// browser tab gets its own nonce, even though the underlying dashboard token
// is shared.
type NonceRotator interface {
	Rotate() (string, error)
}

// NewAuthMiddlewareWithRotator is the rotator-aware constructor. It returns
// the middleware AND a handle that can mint fresh single-use nonces. The
// extra return value is the only way the dashboard's internal bootstrap
// endpoint can reach the live authenticator.
//
// When opts.AllowUnauthenticated is true the returned rotator is a no-op
// (Rotate returns "" with no error) — there is no auth to rotate.
func NewAuthMiddlewareWithRotator(opts AuthOptions) (func(http.Handler) http.Handler, NonceRotator) {
	if opts.Token == "" && !opts.AllowUnauthenticated {
		panic("web.NewAuthMiddleware: empty Token requires explicit AllowUnauthenticated=true; refusing to start an unauthenticated dashboard")
	}
	if opts.AllowUnauthenticated {
		return func(next http.Handler) http.Handler { return next }, noopRotator{}
	}
	a := &authenticator{
		token:      opts.Token,
		tokenBytes: []byte(opts.Token),
		nonce:      opts.BootstrapNonce,
	}
	return a.wrap, a
}

// noopRotator implements NonceRotator for AllowUnauthenticated deployments.
type noopRotator struct{}

func (noopRotator) Rotate() (string, error) { return "", nil }

// Rotate generates a fresh bootstrap nonce, atomically replaces the stored
// one, and clears the consumed flag. Used by the loopback-only
// /internal/bootstrap endpoint to mint a per-tab nonce when reusing a
// long-lived daemon.
func (a *authenticator) Rotate() (string, error) {
	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	a.nonceMu.Lock()
	a.nonce = tok
	a.nonceConsumed = false
	a.nonceMu.Unlock()
	return tok, nil
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
		// Defence-in-depth response headers applied to every response.
		// `Referrer-Policy` keeps URL noise (one-time bootstrap nonce)
		// out of outbound Referer headers. The OWASP-baseline trio
		// (X-Frame-Options, X-Content-Type-Options, CSP) defends a
		// browser-facing operator against clickjacking, MIME-sniffing,
		// and script injection on the dashboard pages themselves.
		// Strict-Transport-Security is opt-in to TLS-only deployments
		// via a behind-a-proxy header — we don't set it on the
		// plain-HTTP listener.
		h := w.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'")

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
				// Secure flag is conditional: on a plain-HTTP localhost
				// listener the browser would drop a Secure cookie on
				// every subsequent request, breaking auth silently. We
				// detect HTTPS — direct TLS or X-Forwarded-Proto from a
				// reverse proxy — and only then assert Secure. Reverse-
				// proxied deployments still get the full protection.
				secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
				// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure is asserted conditionally above; unconditional Secure breaks plain-HTTP localhost auth
				http.SetCookie(w, &http.Cookie{
					Name:     TokenCookieName,
					Value:    a.token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					Secure:   secure,
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
