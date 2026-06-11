// internal/memory/mempalace.go
package memory

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// SearchResult represents a single result from a MemPalace search.
type SearchResult struct {
	Text       string  `json:"text"`
	Wing       string  `json:"wing"`
	Room       string  `json:"room"`
	SourceFile string  `json:"source_file"`
	Similarity float64 `json:"similarity"`
}

// searchFunc is the package-level hook used by SearchMemPalace. Tests swap it
// to avoid depending on a real `mempalace` install and to inject latency for
// timeout regression coverage.
var searchFunc = runMemPalaceSearchExec

// SearchMemPalace runs a MemPalace search bounded by ctx and returns parsed
// results. Callers MUST pass a context with a deadline — without one a slow
// MemPalace index can block the caller indefinitely.
func SearchMemPalace(ctx context.Context, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}
	return searchFunc(ctx, query)
}

// runMemPalaceSearchExec is the real implementation: shells out to
// `python3 -m mempalace search <query>` under ctx control.
func runMemPalaceSearchExec(ctx context.Context, query string) ([]SearchResult, error) {
	cmd := exec.CommandContext(ctx, "python3", "-m", "mempalace", "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return ParseSearchResults(string(out)), nil
}

// ParseSearchResults parses the structured output from `mempalace search`.
//
// Expected format:
//
//	[1] wing_name / room_name
//	    Source: filename.md
//	    Match:  0.420
//
//	    Content text here...
func ParseSearchResults(raw string) []SearchResult {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var results []SearchResult
	lines := strings.Split(raw, "\n")

	var current *SearchResult
	var contentLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Header line: [N] wing / room
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			// Save previous result
			if current != nil {
				current.Text = strings.TrimSpace(strings.Join(contentLines, "\n"))
				results = append(results, *current)
			}

			current = &SearchResult{}
			contentLines = nil

			// Parse "[N] wing / room"
			closeBracket := strings.Index(trimmed, "]")
			if closeBracket < 0 {
				continue
			}
			rest := strings.TrimSpace(trimmed[closeBracket+1:])
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 {
				current.Wing = strings.TrimSpace(parts[0])
				current.Room = strings.TrimSpace(parts[1])
			} else if len(parts) == 1 {
				current.Wing = strings.TrimSpace(parts[0])
			}
			continue
		}

		if current == nil {
			continue
		}

		// Source line
		if strings.HasPrefix(trimmed, "Source:") {
			current.SourceFile = strings.TrimSpace(strings.TrimPrefix(trimmed, "Source:"))
			continue
		}

		// Match line
		if strings.HasPrefix(trimmed, "Match:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "Match:"))
			if val, err := strconv.ParseFloat(valStr, 64); err == nil {
				current.Similarity = val
			}
			continue
		}

		// Content lines (skip empty lines before first content)
		if trimmed != "" || len(contentLines) > 0 {
			contentLines = append(contentLines, trimmed)
		}
	}

	// Save last result
	if current != nil {
		current.Text = strings.TrimSpace(strings.Join(contentLines, "\n"))
		results = append(results, *current)
	}

	return results
}

// IsMemPalaceAvailable checks if the mempalace CLI is installed.
func IsMemPalaceAvailable() bool {
	_, err := exec.LookPath("python3")
	if err != nil {
		return false
	}
	cmd := exec.Command("python3", "-m", "mempalace", "--version")
	return cmd.Run() == nil
}
