package llm

import (
	"regexp"
	"sort"
)

// knownModelAliases lists model IDs verified working against their providers
// (see CLAUDE.md "Model ID Compatibility" and the README "Providers & models"
// table). `vxd doctor` validates configured model bindings against this
// STATIC list -- deliberately no live API calls, so doctor is fast and free.
//
// Maintenance rule: when a default in config.DefaultConfig changes, or a new
// provider/model pairing is verified, add it here AND update the docs.
var knownModelAliases = map[string]bool{
	// anthropic (Claude CLI subscription tier -- undated aliases only)
	"claude-opus-4-8":   true,
	"claude-opus-4-7":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
	// codex / OpenAI (ChatGPT-Codex subscription)
	"gpt-5.5": true,
	// google (Gemini CLI / Google AI free tier)
	"gemma-4-27b-it": true,
}

// KnownModelAliases returns the known-good model IDs, sorted ascending.
func KnownModelAliases() []string {
	ids := make([]string, 0, len(knownModelAliases))
	for id := range knownModelAliases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IsKnownModelAlias reports whether id is a verified-working undated model
// alias. Unknown IDs are not necessarily broken (providers add models over
// time) -- doctor surfaces them as a WARNING asking the operator to verify,
// because a typo'd model ID historically surfaced downstream as "agent
// produced no code changes" instead of a model error.
func IsKnownModelAlias(id string) bool {
	return knownModelAliases[id]
}

// datedSnapshotRe matches Anthropic-style dated snapshot suffixes
// (claude-opus-4-20250514, claude-sonnet-4-6-20250620). Dated snapshots
// RETIRE: they worked at release, then started returning HTTP 404 once the
// provider rotated snapshots. Undated aliases never end in an 8-digit
// year-prefixed segment, so anchoring at end-of-string keeps false positives
// away for IDs like gpt-5.5 or gemma-4-27b-it.
var datedSnapshotRe = regexp.MustCompile(`-20\d{6}$`)

// LooksLikeDatedSnapshot reports whether the model ID embeds a dated
// snapshot suffix that will eventually retire to a 404.
func LooksLikeDatedSnapshot(id string) bool {
	return datedSnapshotRe.MatchString(id)
}
