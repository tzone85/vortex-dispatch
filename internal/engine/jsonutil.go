package engine

import (
	"encoding/json"
	"strings"
)

// extractJSON strips markdown code fences and conversational preambles
// from LLM responses so the content can be passed to json.Unmarshal.
// Handles three common LLM output patterns:
//  1. Pure JSON: returned as-is
//  2. Code-fenced JSON (```json ... ```): fence stripped, optional preamble OK
//  3. Preamble + JSON (e.g. "Here you go: [...]"): preamble stripped
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Pattern 2: code fence (anywhere — covers preamble + fenced cases)
	if fenceStart := strings.Index(s, "```"); fenceStart != -1 {
		inner := s[fenceStart+3:]
		// Skip optional language tag (everything up to first newline)
		if nl := strings.Index(inner, "\n"); nl != -1 {
			inner = inner[nl+1:]
		}
		// Strip closing fence
		if fenceEnd := strings.Index(inner, "```"); fenceEnd != -1 {
			inner = inner[:fenceEnd]
		}
		return strings.TrimSpace(inner)
	}

	// Pattern 3: find first JSON delimiter and matching last delimiter
	firstObj := strings.Index(s, "{")
	firstArr := strings.Index(s, "[")
	first := firstObj
	openCh, closeCh := byte('{'), byte('}')
	if firstArr != -1 && (firstObj == -1 || firstArr < firstObj) {
		first = firstArr
		openCh, closeCh = '[', ']'
	}
	if first == -1 {
		return s // no JSON markers — return as-is
	}
	last := strings.LastIndexByte(s, closeCh)
	if last <= first {
		return s
	}
	_ = openCh // reserved for future depth-aware parsing
	return strings.TrimSpace(s[first : last+1])
}

// FlexibleString unmarshals either a JSON string or an array of strings into a
// single string. LLMs inconsistently return acceptance_criteria as either form.
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexibleString(s)
		return nil
	}

	// Try array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = FlexibleString(strings.Join(arr, "\n"))
		return nil
	}

	// Fallback: store raw
	*f = FlexibleString(string(data))
	return nil
}
