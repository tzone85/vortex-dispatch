# Tutorial: Your First Requirement

This hands-on walkthrough takes you through VXD's full pipeline — from submitting a natural-language requirement to seeing merged pull requests.

## Demo

Want to see the workflow before trying it? Generate an animated GIF with [VHS](https://github.com/charmbracelet/vhs):

```bash
brew install vhs   # or: go install github.com/charmbracelet/vhs@latest
vhs docs/demo.tape
```

This produces `docs/demo.gif` showing `vxd init` through `vxd dashboard` in action.

## Setup

Make sure you've completed the [Getting Started](getting-started.md) guide. You need:
- VXD installed and `vxd init` run in a git repository
- At least one AI runtime CLI available (Claude Code recommended)
- GitHub CLI authenticated (`gh auth login`)

For this tutorial, we'll use a simple requirement that produces 2-3 stories.

## Step 1: Submit a Requirement

```bash
vxd req "Add a health check endpoint at GET /healthz that returns 200 OK with a JSON body containing service name, version, and uptime"
```

What happens behind the scenes:
1. VXD emits a `REQ_SUBMITTED` event
2. The **Tech Lead** agent (Claude Opus) decomposes your requirement into stories
3. Each story gets a Fibonacci complexity score (1, 2, 3, 5, 8, or 13)
4. Dependencies between stories are identified and stored as a DAG
5. Events are emitted: `STORY_CREATED` (per story), then `REQ_PLANNED`

You'll see output like:

```
Requirement submitted: REQ-01HXYZ...
Planning via Tech Lead...

Stories created:
  STORY-001  [2] Add /healthz route with basic 200 response
  STORY-002  [3] Implement uptime tracking and version injection
  STORY-003  [2] Add health check integration tests
             (depends on STORY-001, STORY-002)

Dependency graph:
  STORY-001 ──┐
              ├──> STORY-003
  STORY-002 ──┘
```

## Step 2: Check Status

```bash
vxd status
```

This queries the SQLite projection store and shows the current state:

```
Requirement REQ-01HXYZ: "Add a health check endpoint..."
  Status: planned

  Stories:
    STORY-001  [2] Add /healthz route           draft
    STORY-002  [3] Implement uptime tracking     draft
    STORY-003  [2] Add integration tests         draft (blocked)
```

Stories in `draft` are waiting to be dispatched. `blocked` means dependencies haven't been satisfied yet.

## Step 3: Resume to Start Execution

```bash
vxd resume REQ-01HXYZ
```

This triggers the **Dispatcher**, which:
1. Reads the dependency DAG
2. Identifies **Wave 1** — stories with no unmet dependencies (STORY-001, STORY-002)
3. Routes each story by complexity:
   - Complexity 2 (STORY-001) -> **Junior** agent (Claude Haiku / GPT-4o-mini)
   - Complexity 3 (STORY-002) -> **Junior** agent
4. Creates a git worktree and branch per story (`vxd/STORY-001`, `vxd/STORY-002`)
5. Spawns tmux sessions with the configured AI runtime

```
Wave 1: dispatching 2 stories in parallel

  STORY-001 -> junior (haiku-4) session: vxd-STORY-001
  STORY-002 -> junior (gpt-4o-mini) session: vxd-STORY-002

Agents working...
```

## Step 4: Monitor Progress

### Live Dashboard

```bash
vxd dashboard
```

The TUI dashboard shows four panels updated every 2 seconds:

```
┌─ Pipeline ──────────────┐┌─ Agents ──────────────────┐
│ REQ-01HXYZ              ││ junior-01  STORY-001       │
│  STORY-001 ██████░░ wip ││   status: working          │
│  STORY-002 ████░░░░ wip ││ junior-02  STORY-002       │
│  STORY-003 ░░░░░░░░ blk ││   status: working          │
└─────────────────────────┘└────────────────────────────┘
┌─ Activity ──────────────┐┌─ Escalations ──────────────┐
│ 14:02 AGENT_SPAWNED     ││ (none)                      │
│ 14:02 STORY_ASSIGNED    ││                              │
│ 14:02 STORY_STARTED     ││                              │
└─────────────────────────┘└────────────────────────────┘
```

Press `q` to exit the dashboard.

### Quick Status Checks

```bash
# List active agents
vxd agents --status working

# View recent events
vxd events --limit 10

# Check a specific requirement
vxd status --req REQ-01HXYZ
```

## Step 5: Watch the Pipeline Complete

As agents finish, VXD automatically progresses each story through:

1. **Code Review** — A Senior agent reviews the diff via LLM
2. **QA** — Lint, build, and test commands run against the worktree
3. **PR Creation** — `gh pr create` with the configured template
4. **Auto-Merge** — Squash merge + branch cleanup (if `auto_merge: true`)

Once STORY-001 and STORY-002 are merged, Wave 2 kicks off automatically:

```
Wave 2: dispatching 1 story

  STORY-003 -> junior (haiku-4) session: vxd-STORY-003
```

STORY-003 (integration tests) runs now that its dependencies are satisfied.

## Step 6: Verify Completion

```bash
vxd status
```

```
Requirement REQ-01HXYZ: "Add a health check endpoint..."
  Status: completed

  Stories:
    STORY-001  [2] Add /healthz route           merged  PR #42
    STORY-002  [3] Implement uptime tracking     merged  PR #43
    STORY-003  [2] Add integration tests         merged  PR #44
```

### Review the Event Trail

```bash
vxd events --limit 30
```

You'll see the full audit trail: every state transition, every agent action, every QA result.

### Check for Any Escalations

```bash
vxd escalations
```

If any stories needed escalation (agent stuck, repeated QA failures), they appear here with reasons and resolution status.

## Step 7: Cleanup

VXD handles cleanup automatically based on your config, but you can also trigger it manually:

```bash
# Preview what would be cleaned up
vxd gc --dry-run

# Actually clean up
vxd gc
```

This removes merged worktrees and branches past the retention period.

## What You've Learned

| Stage | Command | What Happens |
|-------|---------|--------------|
| Submit | `vxd req "..."` | Tech Lead decomposes into stories |
| Plan | (automatic) | Stories scored, DAG built |
| Dispatch | `vxd resume` | Wave-based parallel assignment |
| Execute | (automatic) | Agents work in tmux sessions |
| Review | (automatic) | Senior LLM reviews diffs |
| QA | (automatic) | Lint + build + test |
| Merge | (automatic) | PR created + auto-merged |
| Cleanup | `vxd gc` | Worktrees pruned, branches deleted |

## Next Steps

- [Workflows](workflows.md) — understand each pipeline stage in depth
- [Configuration](configuration.md) — tune routing, monitoring, and merge behavior
- [Monitoring](monitoring.md) — learn about the watchdog, supervisor, and intervention options
