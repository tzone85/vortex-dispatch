package llm

import "strings"

// StripCodeFences removes leading/trailing markdown code fences (```lang ... ```)
// from LLM output. Many LLM responses wrap content in code fences even when
// instructed not to; this function strips them so callers get clean content.
func StripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove first line (```lang) and last line (```)
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 {
			start := 1
			end := len(lines)
			if strings.TrimSpace(lines[end-1]) == "```" {
				end--
			}
			s = strings.Join(lines[start:end], "\n")
		}
	}
	return s
}
