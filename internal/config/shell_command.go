package config

import (
	"fmt"
	"strings"
)

// shellSubstitutionPatterns are rejected in EVERY mode: command substitution
// is the one construct that turns a static YAML command string into arbitrary
// nested code execution.
var shellSubstitutionPatterns = []struct{ seq, name string }{
	{"$((", "arithmetic expansion"}, // checked before $( so the message is precise
	{"$(", "POSIX command substitution `$(...)`"},
	{"`", "backtick command substitution"},
}

// shellChainPatterns are rejected only in strict mode
// (security.strict_shell_commands: true). Legitimate multi-step commands can
// be expressed with the `command_list` criterion field instead — VXD chains
// the entries itself, so the YAML never needs shell metacharacters.
//
// Multi-character sequences are listed before their single-character prefixes
// so error messages name the most specific construct.
var shellChainPatterns = []struct{ seq, name string }{
	{"2>&1", "stderr redirection `2>&1`"},
	{"&&", "AND chaining `&&`"},
	{"||", "OR chaining `||`"},
	{">>", "append redirection `>>`"},
	{"|", "pipe `|`"},
	{";", "statement chaining `;`"},
	{"&", "background execution `&`"},
	{">", "output redirection `>`"},
	{"<", "input redirection `<`"},
}

// ValidateShellCommand rejects YAML-supplied shell-command strings that could
// smuggle extra commands past the operator. The threat model is an operator
// who pastes a vxd.yaml from a hostile or untrusted source — these command
// strings flow straight to `sh -c`.
//
// Non-strict (default): only command substitution is rejected; pipes and
// chains stay allowed because legitimate QA commands use them
// (`go test ./... | grep PASS`). Strict (security.strict_shell_commands:
// true — recommended for SaaS-hosted / multi-tenant deploys): pipes,
// chaining, background execution, and redirection are rejected too; use
// `command_list` to express multi-step work.
func ValidateShellCommand(cmd string, strict bool) error {
	if cmd == "" {
		return nil
	}
	for _, p := range shellSubstitutionPatterns {
		if strings.Contains(cmd, p.seq) {
			return fmt.Errorf("config-supplied command contains %s; rewrite to avoid embedded expressions", p.name)
		}
	}
	if !strict {
		return nil
	}
	for _, p := range shellChainPatterns {
		if strings.Contains(cmd, p.seq) {
			return fmt.Errorf("config-supplied command contains %s, rejected by security.strict_shell_commands; use command_list to express multi-step commands", p.name)
		}
	}
	return nil
}
