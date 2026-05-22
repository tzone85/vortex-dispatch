package ghost

import (
	"fmt"
	"io"
	"net/http"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
)

// wrapHTTPError maps Ghost HTTP errors to devdb sentinels via errors.Is matching.
// body is pre-read by the caller to allow the response body to be closed first.
func wrapHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case 401, 403:
		return fmt.Errorf("ghost auth (%d): %s: %w", resp.StatusCode, string(body), devdb.ErrProviderDown)
	case 404:
		return fmt.Errorf("ghost not found: %s: %w", string(body), devdb.ErrNotFound)
	case 409:
		return fmt.Errorf("ghost conflict: %s: %w", string(body), devdb.ErrAlreadyExists)
	case 429:
		return fmt.Errorf("ghost rate limit: %s: %w", string(body), devdb.ErrProviderDown)
	case 500, 502, 503, 504:
		return fmt.Errorf("ghost server %d: %s: %w", resp.StatusCode, string(body), devdb.ErrProviderDown)
	default:
		return fmt.Errorf("ghost: %d %s", resp.StatusCode, string(body))
	}
}
