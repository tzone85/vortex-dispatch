---
tags: [architecture, git, recovery]
---

# Conflict Resolution

During the merge step, a story branch is rebased onto the base branch. Conflicts
are resolved by `internal/engine/conflict_resolver.go` using a 3-tier strategy.

## 1. Binary detection
`internal/git/conflict.go`:
- `IsBinaryConflict` runs `git diff --numstat HEAD -- <file>`; output `-\t-\t<path>`
  means binary.
- `SniffBinary` is the fallback (null-byte check in first 8 KB) for newly-added
  unmerged files not yet in HEAD.

Binary files are **never** sent to an LLM.

## 2. Binary policy
- Compiled/oversized files (`server`, `main`, `*.exe`, >500 KB) are removed via
  `git rm` → emits `STORY_CONFLICT_BINARY_REMOVED`.
- Smaller binaries resolve with `git checkout --ours` (story branch wins) →
  `STORY_CONFLICT_BINARY`.

## 3. Senior fast-path → Tech Lead escalation
- Text conflicts go to the **Senior** model. If the result still contains
  `<<<<<<<` markers, it's discarded.
- The **Tech Lead** is tried when the Senior fails, the result still has markers,
  or the conflict spans >3 files (integration-level). Its prompt includes the
  requirement title/text, story acceptance criteria, `depends_on` titles, sibling
  titles, and the last 3 `git log` subjects for the file. Emits
  `STORY_CONFLICT_ESCALATED`.

## Constructor
```
NewConflictResolver(senior, seniorModel, techLead, techLeadModel, maxTokens, projStore, es)
```
Pass `nil, ""` for `techLead/techLeadModel` to disable escalation (senior-only,
used in tests).

## Related
- [[Escalation Chain]] — the story-level (non-conflict) recovery ladder.
- [[Event Sourcing]] — all `STORY_CONFLICT_*` events are projected.
