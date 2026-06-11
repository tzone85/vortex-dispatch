package ghost

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
)

// maxErrorBodyBytes caps how much of a Ghost error body lands in the
// returned error. Ghost's error responses sometimes include DSN
// fragments and other internal state; capping plus redaction keeps
// those out of operator-visible logs and CI job captures.
const maxErrorBodyBytes = 256

// dsnInURLRe matches `<scheme>://user:password@host…`. The password
// segment is the only thing we redact — the rest stays visible so the
// operator can still diagnose the failure.
var dsnInURLRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^\s:/?#]+):[^\s@/?#]+@`)

// safeBody truncates and redacts the body so embedded credentials can't
// leak via log aggregators / CI archives. URL-form DSN passwords are
// rewritten to ***; key/value `password=…` is rewritten to
// `password=***`. Capped at maxErrorBodyBytes so a huge HTML error page
// can't flood the log line.
func safeBody(b []byte) string {
	if len(b) > maxErrorBodyBytes {
		b = b[:maxErrorBodyBytes]
	}
	s := string(b)
	s = dsnInURLRe.ReplaceAllString(s, "$1:***@")
	s = passwordKeyRe.ReplaceAllString(s, "password=***")
	return s
}

var passwordKeyRe = regexp.MustCompile(`password=\S+`)

// wrapHTTPError maps Ghost HTTP errors to devdb sentinels via errors.Is matching.
// body is pre-read by the caller to allow the response body to be closed first.
// Body content is truncated + DSN-redacted before landing in the error string
// to keep upstream Ghost credentials out of operator logs.
func wrapHTTPError(resp *http.Response) error {
	raw, _ := io.ReadAll(resp.Body)
	body := safeBody(raw)
	switch resp.StatusCode {
	case 401, 403:
		return fmt.Errorf("ghost auth (%d): %s: %w", resp.StatusCode, body, devdb.ErrProviderDown)
	case 404:
		return fmt.Errorf("ghost not found: %s: %w", body, devdb.ErrNotFound)
	case 409:
		return fmt.Errorf("ghost conflict: %s: %w", body, devdb.ErrAlreadyExists)
	case 429:
		return fmt.Errorf("ghost rate limit: %s: %w", body, devdb.ErrProviderDown)
	case 500, 502, 503, 504:
		return fmt.Errorf("ghost server %d: %s: %w", resp.StatusCode, body, devdb.ErrProviderDown)
	default:
		return fmt.Errorf("ghost: %d %s", resp.StatusCode, body)
	}
}
