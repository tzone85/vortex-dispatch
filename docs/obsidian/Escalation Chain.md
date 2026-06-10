---
tags: [architecture, recovery]
---

# Escalation Chain

When a story fails, VXD climbs a 5-tier ladder rather than giving up.

```
Tier 0: Same-role retry with smart error analysis (8 categories)
Tier 1: Senior developer (more capable model)
Tier 2: Manager diagnosis (LLM analyzes the failure pattern)
Tier 3: Tech Lead re-planning (decompose into smaller stories)
Tier 4: Pause (human intervention required)
```

Implemented in `internal/engine/escalation.go` (`EscalationMachine`) and driven
from `internal/engine/monitor.go`.

## Tier 3: re-planning safety
The Tech Lead re-planner (`handleTechLeadEscalation` → `planner.RePlan`) builds a
failure context from the story's events and **agent log**. The agent log is
untrusted data, so it is screened with `sanitize.DetectPromptInjection` and
redacted on match before entering the prompt (see [[Security Model]]).

Child stories from a split/re-plan have their IDs and suffixes validated through
`ValidateSplitWithEdges` → `state.ValidateStoryID`, preventing path-traversal IDs
from a hallucinating model.

## Events emitted
- `STORY_ESCALATED` (`from_tier`, `to_tier`, `reason`)
- `STORY_REWRITTEN` — manager rewrote description/acceptance criteria
- `STORY_SPLIT` — Tech Lead decomposed into children

All are recorded in the [[Event Sourcing|event log]]; check the
[[Debugging Checklist]] when stories appear stuck after escalation.

## Related
- [[Conflict Resolution]] has its own Tech-Lead escalation path for rebase conflicts.
