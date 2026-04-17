package engine_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDocCoverage_CLICommands verifies that every CLI command registered in
// root.go is mentioned in CLAUDE.md. This prevents "dark commands" that
// exist in code but are invisible to agents and users.
//
// When this test fails, add the new command to CLAUDE.md's CLI Commands table.
func TestDocCoverage_CLICommands(t *testing.T) {
	rootGo, err := os.ReadFile("../../internal/cli/root.go")
	if err != nil {
		t.Skipf("cannot read root.go: %v (run from repo root)", err)
	}
	claudeMD, err := os.ReadFile("../../CLAUDE.md")
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}

	// Extract command names from rootCmd.AddCommand(newXxxCmd())
	re := regexp.MustCompile(`rootCmd\.AddCommand\(new(\w+)Cmd\(\)\)`)
	matches := re.FindAllStringSubmatch(string(rootGo), -1)

	claudeStr := strings.ToLower(string(claudeMD))

	for _, m := range matches {
		cmdName := strings.ToLower(m[1])
		// Map compound names to their CLI form
		cliForm := mapCmdName(cmdName)

		if !strings.Contains(claudeStr, "vxd "+cliForm) {
			t.Errorf("CLI command 'vxd %s' (from new%sCmd) not documented in CLAUDE.md — add it to the CLI Commands table",
				cliForm, m[1])
		}
	}
}

// TestDocCoverage_ConfigSections verifies that every top-level Config struct
// field is mentioned in README.md's Configuration table. Prevents "dark config"
// fields that users can't discover.
//
// When this test fails, add the new config section to README.md.
func TestDocCoverage_ConfigSections(t *testing.T) {
	configGo, err := os.ReadFile("../../internal/config/config.go")
	if err != nil {
		t.Skipf("cannot read config.go: %v", err)
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Skipf("cannot read README.md: %v", err)
	}

	// Extract yaml tags from Config struct fields
	// Pattern: FieldName SomeType `yaml:"tag_name"`
	re := regexp.MustCompile(`\w+\s+\w+\s+` + "`" + `yaml:"(\w+)"` + "`")

	// Find the Config struct body (between "type Config struct {" and next "}")
	configStr := string(configGo)
	start := strings.Index(configStr, "type Config struct {")
	if start == -1 {
		t.Fatal("cannot find 'type Config struct' in config.go")
	}
	// Find matching closing brace (simple: first } after the struct opening)
	body := configStr[start:]
	braceDepth := 0
	end := 0
	for i, ch := range body {
		if ch == '{' {
			braceDepth++
		} else if ch == '}' {
			braceDepth--
			if braceDepth == 0 {
				end = i
				break
			}
		}
	}
	structBody := body[:end]

	matches := re.FindAllStringSubmatch(structBody, -1)
	readmeStr := strings.ToLower(string(readme))

	// Skip version field (internal, not user-configured)
	skip := map[string]bool{"version": true}

	for _, m := range matches {
		tag := m[1]
		if skip[tag] {
			continue
		}
		if !strings.Contains(readmeStr, "`"+tag+"`") && !strings.Contains(readmeStr, tag) {
			t.Errorf("Config field yaml:\"%s\" not documented in README.md — add it to the Configuration table", tag)
		}
	}
}

// TestDocCoverage_EventTypes verifies that critical user-facing event types
// are documented in CLAUDE.md's Architecture section.
func TestDocCoverage_EventTypes(t *testing.T) {
	eventsGo, err := os.ReadFile("../../internal/state/events.go")
	if err != nil {
		t.Skipf("cannot read events.go: %v", err)
	}
	claudeMD, err := os.ReadFile("../../CLAUDE.md")
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}

	// Critical event types that affect user-visible behavior
	criticalEvents := []string{
		"STORY_ESCALATED",
		"STORY_REWRITTEN",
		"STORY_SPLIT",
		"STORY_SLA_BREACHED",
	}

	eventsStr := string(eventsGo)
	claudeStr := string(claudeMD)

	for _, evt := range criticalEvents {
		// Verify event exists in code
		if !strings.Contains(eventsStr, evt) {
			t.Errorf("critical event %s not found in events.go", evt)
			continue
		}
		// Verify it's documented
		if !strings.Contains(claudeStr, evt) {
			t.Errorf("critical event %s not documented in CLAUDE.md — add it to the Architecture section", evt)
		}
	}
}

// mapCmdName converts CamelCase command constructor names to their CLI form.
func mapCmdName(name string) string {
	switch name {
	case "approveplan":
		return "approve-plan"
	case "rejectplan":
		return "reject-plan"
	default:
		return name
	}
}
