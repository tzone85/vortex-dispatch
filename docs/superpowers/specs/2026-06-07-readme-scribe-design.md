# README Scribe — let VXD finish a requirement by updating the README

**Date:** 2026-06-07
**Status:** Draft
**Author:** Thando Mini (via Claude Opus 4.7)

## TL;DR

When the Tech Lead decomposes a requirement, it auto-appends a final story whose only job is to write or update `README.md`. The story depends on every other story in the requirement, runs through the same review + QA + merge gates as everything else, and is implemented by a dedicated "scribe" role.

Greenfield: scribe authors the full README. Existing project: scribe surgically adds a section describing the work that just merged, never rewriting prose the human wrote.

## Why now

VXD already produces working code, tests, and infrastructure. What it does *not* produce is a clear written record of what was built. On a fresh repo (`webhookd` greenfield demo from today's session), the README at the end of an autonomous run is whatever scaffolding the first story dropped — typically a one-line placeholder. On an existing repo (`TextAdventureGame` revamp), the README still says `# TextAdventureGame` despite three merged PRs adding a Maven scaffold, a parser, and a YAML world loader.

The gap shows up in two LinkedIn-grade outputs:
- A recruiter cloning a VXD-generated repo sees code without context.
- A client reading a delivery report sees commit messages but no human-readable "what was delivered."

This spec closes that gap inside the existing pipeline — no new phase, no new infrastructure.

## Goals

1. **Every requirement leaves the project with a README that reflects what was built.**
2. **Greenfield-aware.** Detect `README.md` absence or near-emptiness; author from scratch with structure (overview, install, usage, architecture, testing, license).
3. **Non-destructive on existing projects.** Never rewrite human prose. Append, or edit only inside scribe-managed markers.
4. **Goes through the same gates as code.** Review checks accuracy, QA checks structure, conflict resolver handles README conflicts.
5. **Cheap when nothing to write.** If the requirement produced no user-visible behaviour change (refactor, bugfix), the scribe story still runs but emits a no-op commit message and merges without README touch.

## Non-goals

- A general-purpose docs generator. Other markdown under `docs/` is out of scope.
- Auto-generating API reference from code (Godoc / JSDoc territory).
- Changelog generation — `vxd report` already does delivery reports; not duplicating.
- Rewriting historical READMEs across the repo's git history.

## Design

### 1. Tech Lead emits a final `scribe` story

`internal/agent/planner.go` already produces a `[]Story` from the requirement. Extend `decomposeRequirement` to always append:

```yaml
id: <reqID>-s-NNN-readme
role: scribe
title: Update README to reflect the work merged in <reqID>
depends_on: <every other story ID in the requirement>
complexity: 2
acceptance_criteria:
  - README.md exists at repo root
  - Every merged story has a corresponding mention (greenfield) or a Recent Changes entry (existing project)
  - No lint/markdown errors (`markdownlint README.md` exits 0)
```

The `scribe` role is new. Routing rule: scribe is always Senior tier (Claude Sonnet) — README quality is end-user-facing.

### 2. Greenfield vs existing detection

The scribe agent's prompt is rendered by `RenderScribePrompt` (new function in `internal/agent/render.go`). Before rendering, the renderer inspects the base branch's `README.md`:

| Detection | Criterion | Mode |
|-----------|-----------|------|
| Greenfield | File absent, OR `< 500 bytes`, OR `< 10 non-blank lines`, OR matches one of: `# <repo-name>` (single-line) | `author` |
| Existing | Anything else | `extend` |

`author` mode prompt: produce a full README using the project's structure (detected from `internal/agent/templates/readme-greenfield.md`).

`extend` mode prompt: identify scribe-managed markers and update only those. If no markers exist yet, add a "Recent Changes" section under a marker and write there. Hand-written prose is verbatim-preserved.

### 3. Scribe-managed markers

A single canonical marker pair the scribe will own forever:

```markdown
<!-- vxd:scribe:start -->
...auto-maintained content...
<!-- vxd:scribe:end -->
```

Everything outside the markers is owned by humans. The scribe agent is prompted with: *if the markers do not exist, append them at the end of the README and write inside. If they exist, replace the contents between them. Never write anywhere else.*

### 4. Inputs the scribe receives

The render-side gives the scribe:

- The full requirement text (`REQUIREMENT.md` from event log).
- The Tech Lead's plan (stories with descriptions + acceptance criteria).
- Per-story merge results: title, description, files changed (paths only, not diffs), test counts.
- The current `README.md` if present.
- The project name, primary language (from `vxd projects` detection), and license SPDX.

It does *not* receive raw diffs — README writes should be at the level of "what this project does now", not "here's what changed line-by-line".

### 5. QA criteria (declarative)

The scribe story's `success_criteria` in `vxd.yaml` (default added by `vxd init`):

```yaml
qa:
  scribe:
    - kind: file_exists
      path: README.md
    - kind: file_contains
      path: README.md
      value: "vxd:scribe:start"
    - kind: command_succeeds
      cmd: "markdownlint README.md"
      allow_missing: true   # markdownlint is optional
```

These are evaluated by the existing `engine.evaluateCriteria` — no new machinery.

### 6. Pipeline impact

```
Existing pipeline (before):
  plan → dispatch → ... → all stories merged → REQ_COMPLETED

With scribe (after):
  plan → dispatch → ... → all code stories merged
                          → scribe story dispatched (Sonnet, scribe role)
                          → review → QA → merge
                          → REQ_COMPLETED
```

Adds ~1–3 minutes of wall time per requirement. No retries on scribe failure beyond tier 1 (re-dispatch with the same scribe role) — if scribe fails twice, mark requirement `awaiting_human_doc` and continue. The code is already merged; the README is best-effort.

### 7. Opt-out

`vxd.yaml`:

```yaml
planning:
  emit_scribe_story: true   # default
```

For clients that maintain their own docs externally, set to `false`. The Tech Lead skips the final story.

## Acceptance scenarios (Given/When/Then)

```
GIVEN a greenfield repo with a one-line README placeholder
WHEN  `vxd req "Build a CLI tool that converts CSV to JSON with streaming, includes tests"` runs to completion
THEN  the final commit on main updates README.md from one line to a structured document
AND   the document contains: overview, install, usage example, testing section, license line
AND   the document references the CLI's actual binary name
AND   the document is between scribe markers OR replaces the placeholder wholesale
```

```
GIVEN an existing project with a hand-written 200-line README
WHEN  `vxd req "Add OAuth2 login flow"` runs to completion
THEN  the existing 200 lines of prose remain byte-for-byte identical
AND   a new "Recent Changes" section appears between scribe markers
AND   the section describes the OAuth flow at user-facing level
AND   the section does not include file paths, line numbers, or diff hunks
```

```
GIVEN a requirement that produces a pure refactor (no behaviour change)
WHEN  the scribe story runs
THEN  the scribe agent emits a commit with message "chore(readme): no user-visible changes in <reqID>"
AND   README.md is unchanged
AND   the story merges (QA passes — file_exists is still true)
```

```
GIVEN the scribe story fails twice
WHEN  the requirement otherwise has all code stories merged
THEN  the requirement status is set to `completed_doc_pending`
AND   an event `STORY_SCRIBE_ABANDONED` is emitted
AND   no further retry is attempted
AND   `vxd report <req-id>` surfaces "README not updated" in the report header
```

## Implementation phases

| Phase | Scope | Wiring tests |
|-------|-------|--------------|
| SP1 | `scribe` role added to routing; planner always appends scribe story; existing tests adjusted to expect +1 story per requirement | `TestPlanner_AppendsScribeStory`, `TestRouting_ScribeIsSenior` |
| SP2 | `RenderScribePrompt` with `author` / `extend` mode detection; greenfield template under `internal/agent/templates/` | `TestScribePrompt_GreenfieldDetection`, `TestScribePrompt_ExtendsExistingReadme` |
| SP3 | Scribe-managed markers respected on `extend`; prose-preservation regression test asserts byte-for-byte equality outside markers | `TestScribe_PreservesHumanProse` |
| SP4 | Declarative QA criteria for the scribe story wired into config + default `vxd init` template | `TestQA_ScribeStorySuccess`, `TestQA_ScribeStoryMissingReadme` |
| SP5 | Tier-1 retry policy; `STORY_SCRIBE_ABANDONED` event + `completed_doc_pending` requirement status; `vxd report` header surfaces it | `TestScribe_AbandonsAfterTwoFails`, `TestReport_FlagsMissingReadmeUpdate` |
| SP6 | NXD port (Ollama-driven scribe — `qwen2.5-coder:14b` is plenty for README work) | NXD `TestScribe_Ollama` |

Each SP is one PR. SP1–SP3 are net-new behaviour; SP4–SP6 are productionisation.

## Open questions

1. **Should the scribe also touch `CHANGELOG.md` when present?** Lean no — `vxd report` is the changelog story. Out of scope for v1.
2. **What about `docs/` markdown files referenced from the README?** Out of scope. Scribe touches only `README.md`. A future "docs maintainer" role could expand later.
3. **Should scribe run on `--no-dispatch` planning?** No — it would be planned (one of the listed stories) but not dispatched until `vxd resume`. Matches existing behaviour for every other story.
4. **Custom marker names per project?** Defer. Hardcode `vxd:scribe:start` / `vxd:scribe:end` for v1; revisit if a client asks.

## Risk register

| Risk | Mitigation |
|------|------------|
| Scribe hallucinates features that weren't actually built | Review-stage LLM check: scribe output must match the merged story titles + acceptance criteria. Mismatches fail review. |
| Scribe overwrites hand-written README on a project that hasn't seen scribe before | Marker-only writes in `extend` mode. Greenfield detection thresholds are conservative (placeholder-shaped only). |
| Scribe story blocks REQ_COMPLETED indefinitely on review thrash | Tier-1 max-retries already enforced; abandonment path (`completed_doc_pending`) is the safety valve. |
| Repos that intentionally have a minimal README (e.g. monorepos pointing elsewhere) get treated as greenfield | Opt-out flag (`planning.emit_scribe_story: false`) per project. |

## What changes in the user-facing experience

Before:
```
$ vxd req "Build a webhook ingestion service"
... pipeline runs ...
$ cat README.md
# webhookd
```

After:
```
$ vxd req "Build a webhook ingestion service"
... pipeline runs ...
$ cat README.md
# webhookd

A hardened HTTP service for receiving and forwarding webhooks ...

## Install
go install github.com/<org>/webhookd@latest

## Usage
... (and so on, generated from the actual requirement + merged work)

## Architecture
... (one-paragraph summary derived from story titles)

<!-- vxd:scribe:start -->
Generated by VXD on 2026-06-07 from requirement 01KTHB1RHM7WBKP3ZFFP6EB42Z.
<!-- vxd:scribe:end -->
```

The greenfield demo (`webhookd`) and the revamp (`TextAdventureGame`) would both end this session with a proper README, not a placeholder.
