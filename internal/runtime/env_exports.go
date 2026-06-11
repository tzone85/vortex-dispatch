package runtime

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

// envKeyPattern matches a POSIX-compliant environment variable name:
// first character must be a letter or underscore, the rest may include
// digits. Anything else is rejected to prevent shell injection through
// keys like "FOO; rm -rf".
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateEnvKey rejects environment variable names that contain shell
// metacharacters or otherwise break POSIX naming. Returns nil for valid
// keys; a descriptive error otherwise.
func ValidateEnvKey(name string) error {
	if name == "" {
		return fmt.Errorf("env var name must not be empty")
	}
	if !envKeyPattern.MatchString(name) {
		return fmt.Errorf("env var name %q contains invalid characters (POSIX: [A-Za-z_][A-Za-z0-9_]*)", name)
	}
	return nil
}

// BuildEnvExports renders `env` as a deterministic POSIX shell prefix of
// the form `export KEY='value'; export ...; `.
//
// Security contract:
//
//   - Keys are validated with ValidateEnvKey. Invalid keys are SKIPPED
//     (logged at warning level) so a single bad config does not block
//     the entire dispatch path.
//   - Values are single-quoted via QuoteShellArg. POSIX single quotes
//     suppress all shell expansion ($var, $(cmd), backticks, !), so any
//     value — including attacker-controlled DSNs or YAML config — is
//     emitted verbatim and cannot inject commands.
//
// Use this in place of `fmt.Sprintf("export %s=%q; ", key, val)`, which
// uses Go's double-quote escaping and leaves $, `, ! active under sh.
//
// Output is sorted by key for stable testing.
func BuildEnvExports(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		if err := ValidateEnvKey(k); err != nil {
			log.Printf("[runtime] dropping env var with unsafe name: %v", err)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString("export ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(QuoteShellArg(env[k]))
		b.WriteString("; ")
	}
	return b.String()
}
