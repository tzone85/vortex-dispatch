package dashstart

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// HTTPDoer abstracts the HTTP probe so tests can stub it without binding
// to a real port. *http.Client satisfies this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// AliveInfo describes a successfully probed running dashboard daemon.
type AliveInfo struct {
	PID  int
	Port int
}

// ErrNotRunning is returned when no daemon is detected (no pidfile, dead
// process, or unreachable healthz). Callers use errors.Is to branch on
// "do I need to spawn?" without inspecting the wrapped cause.
var ErrNotRunning = errors.New("dashboard daemon not running")

// IsAlive checks for a running dashboard daemon. The healthz probe is the
// authoritative signal: an HTTP 200 from http://localhost:<defaultPort>/health
// means we have a live daemon (regardless of whether the pidfile is stale).
//
// The pidfile is used only to report the PID back to the caller for diagnostic
// output. If the pidfile is missing, malformed, or refers to a dead PID, we
// fall back to probing defaultPort directly — a dashboard running under a
// different supervisor (e.g. launchd) still counts as "alive".
//
// Returns ErrNotRunning if no daemon answers within ctx.
func IsAlive(ctx context.Context, doer HTTPDoer, pidfile string, defaultPort int) (AliveInfo, error) {
	info := AliveInfo{Port: defaultPort}

	if data, err := os.ReadFile(pidfile); err == nil {
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil {
			info.PID = pid
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/health", defaultPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AliveInfo{}, fmt.Errorf("build healthz request: %w", err)
	}

	resp, err := doer.Do(req)
	if err != nil {
		return AliveInfo{}, fmt.Errorf("%w: healthz: %v", ErrNotRunning, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return AliveInfo{}, fmt.Errorf("%w: healthz status %d", ErrNotRunning, resp.StatusCode)
	}

	return info, nil
}

// WaitHealthy polls IsAlive until it returns nil or ctx is done. It is used
// after Spawn to confirm the freshly-launched daemon has bound its port.
//
// Polls at interval (default 100ms) and gives up at timeout (default 3s).
// The defaults match what's safe for `vxd req`: long enough for a cold
// boot of the dashboard, short enough not to feel like a hang.
func WaitHealthy(ctx context.Context, doer HTTPDoer, pidfile string, port int, interval, timeout time.Duration) (AliveInfo, error) {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		probeCtx, cancel := context.WithTimeout(ctx, interval)
		info, err := IsAlive(probeCtx, doer, pidfile, port)
		cancel()
		if err == nil {
			return info, nil
		}

		if time.Now().After(deadline) {
			return AliveInfo{}, fmt.Errorf("dashboard daemon did not become healthy within %s: %w", timeout, err)
		}

		select {
		case <-ctx.Done():
			return AliveInfo{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
