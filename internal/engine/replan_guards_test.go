package engine

import "testing"

func TestExtractLexicalAnchors_DropsShortAndStopwords(t *testing.T) {
	anchors := ExtractLexicalAnchors(
		"Add health check endpoint to the API gateway",
		"Story: implement a feature for tracking event_store writes",
	)

	// Should contain meaningful identifiers >= 5 chars.
	for _, want := range []string{"health", "endpoint", "gateway", "tracking", "event_store"} {
		if _, ok := anchors[want]; !ok {
			t.Errorf("expected anchor %q in set %v", want, anchors)
		}
	}

	// Should drop short tokens, common verbs, common nouns.
	for _, banned := range []string{"add", "the", "to", "story", "implement", "feature", "file"} {
		if _, ok := anchors[banned]; ok {
			t.Errorf("did not expect %q in anchor set %v", banned, anchors)
		}
	}
}

func TestHasLexicalGrounding_AllowsRelatedSubstory(t *testing.T) {
	parent := ExtractLexicalAnchors(
		"Add Postgres ephemeral devdb per story",
		"Each story gets its own database; isolation by template fork.",
	)
	if !HasLexicalGrounding(parent, "Wire devdb lifecycle into executor") {
		t.Error("expected related sub-story to be grounded")
	}
	if !HasLexicalGrounding(parent, "Add SLA-breach release path for ephemeral databases") {
		t.Error("expected SLA-breach sub-story to be grounded via 'ephemeral'")
	}
}

func TestHasLexicalGrounding_RejectsUnrelatedSubstory(t *testing.T) {
	parent := ExtractLexicalAnchors(
		"Add Postgres ephemeral devdb per story",
		"Each story gets its own database; isolation by template fork.",
	)
	// An LLM hallucination — unrelated topic, no shared meaningful token.
	if HasLexicalGrounding(parent, "Implement OAuth login flow for marketing landing page") {
		t.Error("expected unrelated sub-story to fail grounding")
	}
	if HasLexicalGrounding(parent, "Migrate the front-end dashboard to Vue.js components") {
		t.Error("expected dashboard-migration sub-story to fail grounding (no devdb/postgres/etc.)")
	}
}

func TestHasLexicalGrounding_EmptyParentReturnsTrue(t *testing.T) {
	// No parent anchors means no grounding source; refuse to over-block.
	if !HasLexicalGrounding(map[string]struct{}{}, "anything goes") {
		t.Error("expected true when parent anchors empty")
	}
}

func TestHasLexicalGrounding_RespectsMinLength(t *testing.T) {
	parent := ExtractLexicalAnchors("Devdb fork should be POSIX-safe")
	// Single short token "be" doesn't count.
	if HasLexicalGrounding(parent, "Be careful") {
		t.Error("short tokens must not count as grounding")
	}
}
