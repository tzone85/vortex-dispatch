package engine

import (
	"strings"
	"unicode"
)

// minLexicalAnchorLen is the shortest token a sub-story title/description
// can share with the parent and still count as grounded. Short tokens
// (4 chars or fewer) carry too much vocabulary noise — "test", "code",
// "add", "the" appear everywhere.
const minLexicalAnchorLen = 5

// lexicalStopwords are tokens that wouldn't be informative grounding
// even if they pass the length filter. Domain-agnostic only; project
// vocabulary stays in to avoid hand-tuning.
var lexicalStopwords = map[string]struct{}{
	"story": {}, "stories": {}, "implement": {}, "implementation": {},
	"feature": {}, "function": {}, "method": {}, "class": {}, "file": {},
	"files": {}, "should": {}, "would": {}, "could": {}, "shall": {},
	"system": {}, "service": {}, "module": {}, "package": {}, "thing": {},
	"thing.": {}, "above": {}, "below": {}, "right": {}, "wrong": {},
}

// ExtractLexicalAnchors returns the set of tokens (lowercased, no
// punctuation) that are ≥ minLexicalAnchorLen and not in
// lexicalStopwords. Used to derive a grounding vocabulary from parent
// requirement/story text.
func ExtractLexicalAnchors(texts ...string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, t := range texts {
		for _, tok := range tokenize(t) {
			if len(tok) < minLexicalAnchorLen {
				continue
			}
			if _, banned := lexicalStopwords[tok]; banned {
				continue
			}
			out[tok] = struct{}{}
		}
	}
	return out
}

// HasLexicalGrounding reports whether any token in candidate texts
// overlaps with the parent anchor set. The check is permissive — one
// shared token is enough — but catches the egregious "we replanned
// into a totally unrelated feature" failure mode without snaring
// legitimate decompositions.
//
// Returns true also when parentAnchors is empty (no grounding source to
// check against — refuse to over-block in low-information cases).
func HasLexicalGrounding(parentAnchors map[string]struct{}, candidateTexts ...string) bool {
	if len(parentAnchors) == 0 {
		return true
	}
	for _, t := range candidateTexts {
		for _, tok := range tokenize(t) {
			if len(tok) < minLexicalAnchorLen {
				continue
			}
			if _, ok := parentAnchors[tok]; ok {
				return true
			}
		}
	}
	return false
}

// tokenize lowercases, drops punctuation, and splits on whitespace.
// Internal helper for the grounding check.
func tokenize(s string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '-':
			// Keep snake_case / kebab-case tokens unsplit so identifiers
			// like "event_store" or "tech-lead" survive intact.
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}
