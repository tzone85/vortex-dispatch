package llm

import "strings"

// claudeEnvExclusions lists environment variable keys that must be stripped
// before spawning a claude CLI subprocess. Keeping these in the child process
// causes either API-credit charges instead of subscription use
// (ANTHROPIC_API_KEY) or nested-session errors (CLAUDECODE).
var claudeEnvExclusions = []string{
	"ANTHROPIC_API_KEY",
	"CLAUDECODE",
}

// FilterClaudeEnv returns a copy of environ with ANTHROPIC_API_KEY and
// CLAUDECODE removed. This ensures that:
//   - Claude CLI uses the user's subscription (Max) rather than API credits.
//   - Spawning claude from within an existing Claude Code session does not
//     trigger "nested session" errors.
func FilterClaudeEnv(environ []string) []string {
	skip := make(map[string]bool, len(claudeEnvExclusions))
	for _, k := range claudeEnvExclusions {
		skip[k] = true
	}
	out := make([]string, 0, len(environ))
	for _, e := range environ {
		if idx := strings.IndexByte(e, '='); idx > 0 && skip[e[:idx]] {
			continue
		}
		out = append(out, e)
	}
	return out
}
