package engine_test

import (
	"os"
	"path/filepath"
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

// TestDocCoverage_SubCommands verifies that every sub-command registered via
// cmd.AddCommand(newXxxCmd()) inside a parent command function is documented
// in CLAUDE.md. This catches "dark sub-commands" — e.g. `vxd opportunity list`
// exists in code but isn't visible to agents or users reading CLAUDE.md.
//
// Detection strategy:
//  1. Scan all .go files in internal/cli/ for parent-command functions that
//     call cmd.AddCommand(newXxxCmd()).
//  2. For each such parent, read its Use: field to get the CLI noun.
//  3. For each registered sub-command, read its Use: field to get the verb.
//  4. Assert that CLAUDE.md contains "vxd <parent> <verb>".
func TestDocCoverage_SubCommands(t *testing.T) {
	cliDir := filepath.Join("..", "..", "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		t.Skipf("cannot read cli dir: %v", err)
	}

	claudeMD, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}
	claudeStr := strings.ToLower(string(claudeMD))

	// Regexps
	reFuncDecl := regexp.MustCompile(`(?m)^func (new\w+Cmd)\(\)`)
	reAddCmd := regexp.MustCompile(`cmd\.AddCommand\((new\w+Cmd)\(\)\)`)
	reUseField := regexp.MustCompile(`Use:\s*"([^"]+)"`)

	type funcInfo struct {
		body string // raw source between function signature and final return
		use  string // first word of Use: field
	}

	// Pass 1: collect the source body of every newXxxCmd function across all files.
	funcBodies := map[string]funcInfo{} // "newXxxCmd" -> funcInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cliDir, e.Name()))
		if err != nil {
			continue
		}
		src := string(raw)

		// Find each "func newXxxCmd() …" and extract its body up to the first
		// closing brace at depth 0 relative to the opening brace.
		locs := reFuncDecl.FindAllStringSubmatchIndex(src, -1)
		for _, loc := range locs {
			name := src[loc[2]:loc[3]] // capture group 1

			// Find opening brace after the function signature.
			start := loc[1]
			braceStart := strings.Index(src[start:], "{")
			if braceStart == -1 {
				continue
			}
			braceStart += start // absolute index

			// Walk to find matching closing brace.
			depth, end := 0, braceStart
			for i, ch := range src[braceStart:] {
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						end = braceStart + i
						break
					}
				}
			}
			body := src[braceStart : end+1]

			// Extract Use: value (first word only — strip args like "<id>").
			use := ""
			if m := reUseField.FindStringSubmatch(body); m != nil {
				use = strings.Fields(m[1])[0] // first word is the CLI token
			}
			funcBodies[name] = funcInfo{body: body, use: use}
		}
	}

	// Pass 2: find parent commands — those whose body contains cmd.AddCommand.
	for parentFunc, info := range funcBodies {
		subMatches := reAddCmd.FindAllStringSubmatch(info.body, -1)
		if len(subMatches) == 0 {
			continue // leaf command, skip
		}
		parentUse := info.use
		if parentUse == "" {
			t.Logf("skipping %s: no Use: field found", parentFunc)
			continue
		}

		for _, sm := range subMatches {
			subFunc := sm[1] // e.g. "newOppListCmd"
			subInfo, ok := funcBodies[subFunc]
			if !ok {
				t.Logf("skipping %s: func body not found", subFunc)
				continue
			}
			subUse := subInfo.use
			if subUse == "" {
				t.Logf("skipping %s: no Use: field found", subFunc)
				continue
			}

			needle := "vxd " + parentUse + " " + subUse
			if !strings.Contains(claudeStr, needle) {
				t.Errorf("CLAUDE.md missing sub-command documentation: `%s` (from %s → %s) — add it to the CLI Commands table",
					needle, parentFunc, subFunc)
			}
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
