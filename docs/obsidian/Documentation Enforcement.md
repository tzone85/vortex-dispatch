---
tags: [quality, documentation]
---

# Documentation Enforcement

Documentation is kept in sync with code by wiring tests in
`internal/engine/doc_coverage_test.go`. Every behavioral change **must** update
docs.

## What's enforced
- `TestDocCoverage_CLICommands` — every `newXxxCmd()` registered in
  `internal/cli/root.go` must appear as `vxd <cmd>` in `CLAUDE.md`.
- `TestDocCoverage_ConfigSections` — every top-level `Config` struct field must
  appear in the `README.md` Configuration table.

## What counts as behavioral (needs docs)
- New CLI command → `CLAUDE.md` CLI table + `README.md`
- New config field → `CLAUDE.md` Config section + `README.md` table
- New user-facing event type → architecture section
- Changed default values → update everywhere the old default appears

## What does **not** need docs
- Internal refactors with no user-visible change
- Test-only changes
- Performance improvements that don't change behavior
- Bug fixes that restore already-documented behavior

## Practical note
When you add `newRebuildCmd` (for example), add a `vxd rebuild` row to
`CLAUDE.md` and `README.md` **before** committing, or `TestDocCoverage_CLICommands`
fails. This is exactly how `vxd rebuild` was added during the
[[Security Audit 2026-06]].

## Related
- [[Testing Conventions]] · [[CLI Commands]] · [[Configuration]]
