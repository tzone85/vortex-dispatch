package engine_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDocCoverage_CLICommands verifies that every top-level CLI command
// registered in root.go is mentioned in the public CLAUDE.md contributor guide.
// Flag/subcommand detail is intentionally delegated to Cobra help and README/docs
// so CLAUDE.md does not become an internal strategy notebook again.
func TestDocCoverage_CLICommands(t *testing.T) {
	rootGo, err := os.ReadFile("../../internal/cli/root.go")
	if err != nil {
		t.Skipf("cannot read root.go: %v (run from repo root)", err)
	}
	claudeMD, err := os.ReadFile("../../CLAUDE.md")
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}

	re := regexp.MustCompile(`rootCmd\.AddCommand\(new(\w+)Cmd\(\)\)`)
	matches := re.FindAllStringSubmatch(string(rootGo), -1)
	claudeStr := strings.ToLower(string(claudeMD))

	for _, m := range matches {
		cmdName := strings.ToLower(m[1])
		cliForm := mapCmdName(cmdName)
		if !strings.Contains(claudeStr, "vxd "+cliForm) {
			t.Errorf("CLI command 'vxd %s' (from new%sCmd) not documented in CLAUDE.md",
				cliForm, m[1])
		}
	}
}

// TestDocCoverage_ConfigSections verifies that every top-level Config struct
// field is mentioned in README.md's Configuration documentation.
func TestDocCoverage_ConfigSections(t *testing.T) {
	configGo, err := os.ReadFile("../../internal/config/config.go")
	if err != nil {
		t.Skipf("cannot read config.go: %v", err)
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Skipf("cannot read README.md: %v", err)
	}

	re := regexp.MustCompile(`\w+\s+\w+\s+` + "`" + `yaml:"(\w+)"` + "`")
	configStr := string(configGo)
	start := strings.Index(configStr, "type Config struct {")
	if start == -1 {
		t.Fatal("cannot find 'type Config struct' in config.go")
	}
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
	skip := map[string]bool{"version": true}

	for _, m := range matches {
		tag := m[1]
		if skip[tag] {
			continue
		}
		if !strings.Contains(readmeStr, "`"+tag+"`") && !strings.Contains(readmeStr, tag) {
			t.Errorf("Config field yaml:\"%s\" not documented in README.md", tag)
		}
	}
}

// TestDocCoverage_EventTypes verifies that critical user-facing event types are
// documented in the public architecture section without requiring private
// implementation notes.
func TestDocCoverage_EventTypes(t *testing.T) {
	eventsGo, err := os.ReadFile("../../internal/state/events.go")
	if err != nil {
		t.Skipf("cannot read events.go: %v", err)
	}
	claudeMD, err := os.ReadFile("../../CLAUDE.md")
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}

	criticalEvents := []string{
		"STORY_ESCALATED",
		"STORY_REWRITTEN",
		"STORY_SPLIT",
		"STORY_SLA_BREACHED",
	}
	eventsStr := string(eventsGo)
	claudeStr := string(claudeMD)

	for _, evt := range criticalEvents {
		if !strings.Contains(eventsStr, evt) {
			t.Errorf("critical event %s not found in events.go", evt)
			continue
		}
		if !strings.Contains(claudeStr, evt) {
			t.Errorf("critical event %s not documented in CLAUDE.md", evt)
		}
	}
}

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
