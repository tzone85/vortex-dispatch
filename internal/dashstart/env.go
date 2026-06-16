package dashstart

import (
	"os"
	"strings"
)

// stripKeys are environment variables we do NOT want the dashboard daemon
// to inherit. ANTHROPIC_API_KEY would silently switch the daemon's LLM
// behavior away from the user's Claude CLI subscription; CLAUDECODE
// confuses Claude CLI plugin detection. Neither belongs in the dashboard's
// (read-only) process.
var stripKeys = map[string]bool{
	"ANTHROPIC_API_KEY": true,
	"CLAUDECODE":        true,
}

// FilteredEnv returns the current process environment with sensitive keys
// stripped. The result is suitable for assigning to exec.Cmd.Env.
//
// Note: this filters by KEY prefix-match-free exact name, so a custom env
// variable that simply contains "ANTHROPIC_API_KEY" as a substring is
// preserved. We only drop the exact, known-problematic names.
func FilteredEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		if stripKeys[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
