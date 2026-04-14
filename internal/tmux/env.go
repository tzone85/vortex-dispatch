package tmux

import (
	"log"
	"os"
	"strings"
)

// criticalEnvVars lists environment variables that must be propagated into the
// tmux global environment before spawning agent sessions. Without this, the
// tmux server may hold stale values from the time it was first started,
// causing agents to authenticate with expired or wrong API keys.
var criticalEnvVars = []string{
	"OPENAI_API_KEY",
	"OLLAMA_HOST",
}

// removeFromTmuxEnv lists environment variables that must be REMOVED from the
// tmux global environment. ANTHROPIC_API_KEY is removed because Claude Code
// agents should use the user's Max subscription (OAuth) rather than API
// credits. If the key is present, Claude CLI uses it instead of OAuth,
// causing "credit balance too low" failures even when the user has an
// active Max subscription.
var removeFromTmuxEnv = []string{
	"ANTHROPIC_API_KEY",
}

// PropagateEnv reads the listed environment variables from the current process
// and sets them in the tmux global environment via `tmux set-environment -g`.
// Variables that are unset in the current process are removed from the tmux
// global environment so agents don't inherit stale values.
//
// Errors are logged but not returned; a failure to propagate one variable
// should not prevent session creation.
func PropagateEnv(vars []string) {
	for _, key := range vars {
		val, ok := os.LookupEnv(key)
		if ok {
			if err := run("set-environment", "-g", key, val); err != nil {
				// Suppress warnings when no tmux server is running yet —
				// CreateSession will start the server and the env vars will
				// be passed directly to the session command.
				if !isNoServerError(err) {
					log.Printf("tmux: warning: failed to set-environment %s: %v", key, err)
				}
			}
		} else {
			// Remove stale value from tmux global env; ignore errors
			// (e.g. variable was never set in tmux, or no server running).
			_ = run("set-environment", "-g", "-u", key)
		}
	}
}

// isNoServerError returns true if the error indicates no tmux server is running.
func isNoServerError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "no current session") ||
		strings.Contains(msg, "server not found")
}

// PropagateCriticalEnv is a convenience wrapper that propagates all
// critical environment variables (API keys, host overrides) into the
// tmux global environment, and removes blocklisted variables that
// would interfere with agent authentication.
func PropagateCriticalEnv() {
	PropagateEnv(criticalEnvVars)

	// Remove blocklisted variables from tmux global env so agents
	// don't inherit them from the parent shell.
	for _, key := range removeFromTmuxEnv {
		if err := run("set-environment", "-g", "-u", key); err != nil {
			if !isNoServerError(err) {
				log.Printf("tmux: warning: failed to unset %s: %v", key, err)
			}
		}
	}
}
