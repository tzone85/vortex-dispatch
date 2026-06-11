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
)

// TokenCookieName is the cookie set on the first authenticated request so
// the browser session carries the token without exposing it in every URL.
const TokenCookieName = "vxd_dashboard_token"

// TokenQueryParam is the URL query parameter accepted on the initial
// browser load to bootstrap the cookie. Subsequent requests are
// expected to use the Authorization header or the cookie.
const TokenQueryParam = "token"

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

// RequireToken wraps next with a bearer-token check. Requests that match
// authBypassedPaths skip the check entirely. The check accepts the token
// in any of three places, in order:
//
//  1. Authorization: Bearer <token>
//  2. ?token=<token> query param (first browser load only; on success
//     the token is also set as a same-site cookie for subsequent requests)
//  3. The TokenCookieName cookie
//
// Constant-time comparison avoids leaking timing info about partial
// match length.
func RequireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		// Defensive: empty token disables auth. NewServer is expected to
		// fatal before reaching here, but be explicit so a misconfigured
		// caller doesn't silently expose endpoints.
		return next
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authBypassedPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Authorization header.
		if header := r.Header.Get("Authorization"); header != "" {
			if strings.HasPrefix(header, "Bearer ") {
				got := []byte(strings.TrimPrefix(header, "Bearer "))
				if subtle.ConstantTimeCompare(got, want) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// 2. ?token query param (bootstrap browser session).
		if q := r.URL.Query().Get(TokenQueryParam); q != "" {
			if subtle.ConstantTimeCompare([]byte(q), want) == 1 {
				http.SetCookie(w, &http.Cookie{
					Name:     TokenCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				next.ServeHTTP(w, r)
				return
			}
		}

		// 3. Cookie set by a previous successful ?token bootstrap.
		if c, err := r.Cookie(TokenCookieName); err == nil && c.Value != "" {
			if subtle.ConstantTimeCompare([]byte(c.Value), want) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="vxd-dashboard"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
