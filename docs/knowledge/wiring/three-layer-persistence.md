# Wiring Persistence — Three Layers

**Why this matters:** features regress when knowledge of how they connect through
the codebase is lost. VXD enforces wiring through three complementary mechanisms.

## Layer 1 — Wiring Tests (enforcement)

Location: internal/engine/wiring_test.go (1500+ lines, 30+ tests)
Purpose: assert features are *activated*, not just implemented.

Pattern:
1. Construct a real store / projector / runtime / cobra root
2. Exercise the feature end-to-end with minimal stubs
3. Assert the observable side effect (status change, event emitted, prompt content)

Examples:
- TestWiring_StoryResetEvent_UpdatesProjection — verifies sqlite.go handles
  EventStoryReset; without this test the default-WARNING branch silently swallowed it
- TestWiring_AutoresearchEvents_Projected — same pattern for all 11 new
  autoresearch events
- TestWiring_AutoresearchCLI_Registered — runs `vxd autoresearch --help` and
  greps for every subcommand by name

Rule from CLAUDE.md: "Every new feature that modifies agent behavior MUST have a
wiring test here that proves the feature activates under real conditions."

## Layer 2 — Default-WARNING Trap (enforcement)

Location: internal/state/sqlite.go Project()
Pattern: every event type has an explicit `case` in the switch; the `default`
branch is `log.Printf("[projector] WARNING: unhandled event type ...")`

Why a trap and not a panic: VXD must not crash on a new event from a future
binary on shared state. WARNING surfaces it loudly; wiring tests then catch it
in CI before merge.

Rule from CLAUDE.md: "New event types MUST be handled in sqlite.go Project()
switch — the default case silently ignores them. Always add a wiring test."

## Layer 3 — Doc Coverage Tests (enforcement + discoverability)

Location: internal/engine/doc_coverage_test.go
- TestDocCoverage_CLICommands — scans internal/cli/root.go for newXxxCmd();
  every command must appear verbatim in CLAUDE.md CLI Commands table
- TestDocCoverage_ConfigSections — every top-level Config struct field (yaml
  tag) must appear in README.md Configuration table

Effect: you cannot add a CLI command or config field without updating user-facing
docs. Caught in this session when AutoresearchConfig was added without README
update — failed locally, fixed in same commit.

## What's NOT covered by these layers

The static checks prove "the wires exist". They don't prove "the agent knows the
wires exist or why". That gap is what MemPalace fills:

- Patterns ("fail-closed on judging")
- Gotchas ("filepath.Match doesn't expand **")
- Why decisions were made
- What v1 placeholders exist and when they're scheduled to be filled

The wiring tests are the seatbelt; MemPalace is the driver's manual.
