# Agents and Roles

VXD models a full agile development team with specialized AI agent roles. Each role has a defined responsibility, a bound LLM model, and specific prompts that guide its behavior.

## Role Hierarchy

```
                    ┌──────────────┐
                    │  Tech Lead   │  Requirement decomposition
                    │  (Opus)      │  Story planning + DAG
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
       ┌──────────┐ ┌────────────┐ ┌────────┐
       │  Senior  │ │Intermediate│ │ Junior │  Implementation
       │ (Sonnet) │ │  (Haiku)   │ │(Haiku) │  by complexity
       └────┬─────┘ └─────┬──────┘ └───┬────┘
            │             │             │
            ▼             ▼             ▼
       ┌──────────────────────────────────────┐
       │             QA (Sonnet)              │  Lint + Build + Test
       └──────────────────┬───────────────────┘
                          │
                    ┌─────▼──────┐
                    │ Supervisor │  Drift detection
                    │  (Sonnet)  │  Reprioritization
                    └────────────┘
```

## Roles in Detail

### Tech Lead

**Responsibility:** Receives natural-language requirements and decomposes them into atomic, testable stories with complexity scores and dependency relationships.

**When active:** During the Planning stage (`vxd req`)

**System prompt instructs the Tech Lead to:**
- Decompose into independently-implementable stories
- Assign Fibonacci complexity (1, 2, 3, 5, 8, 13)
- Identify cross-story dependencies explicitly
- Ensure each story has clear acceptance criteria
- Output structured JSON

**Model recommendation:** Opus — decomposition requires deep reasoning about architecture and dependencies.

### Senior

**Responsibility:** Handles complex stories (6+ complexity points) and reviews code produced by Intermediate and Junior agents.

**When active:** During Execution (complex stories) and Review stages

**System prompt instructs the Senior to:**
- Create feature branches before starting work
- Write clean, tested code following existing patterns
- Escalate blockers after 2 failed attempts
- During review: evaluate against acceptance criteria with severity ratings

**Model recommendation:** Sonnet — good balance of quality and cost for most reviews. Opus if you need maximum review thoroughness.

### Intermediate

**Responsibility:** Handles medium-complexity stories (4-5 points).

**When active:** During Execution stage

**System prompt instructs the Intermediate to:**
- Create a feature branch: `vxd/<story-id>`
- Implement the story completely
- Write tests for changes
- Escalate to Senior if stuck after 2 attempts

**Model recommendation:** Haiku for cost efficiency, Sonnet if quality is paramount.

### Junior

**Responsibility:** Handles simple stories (1-3 points).

**When active:** During Execution stage

**System prompt instructs the Junior to:**
- Create a feature branch: `vxd/<story-id>`
- Implement step by step
- Write tests for changes
- Ask for help if stuck

**Model recommendation:** Haiku or GPT-4o-mini — simple stories don't need expensive models.

### QA

**Responsibility:** Runs quality checks on completed stories and verifies acceptance criteria.

**When active:** During QA stage (after review passes)

**QA checks (sequential):**
1. Lint — configured lint command (e.g., `golangci-lint run`)
2. Build — configured build command (e.g., `go build ./...`)
3. Test — configured test command (e.g., `go test ./...`)

**Model recommendation:** Sonnet — needs good judgment for acceptance criteria verification.

### Supervisor

**Responsibility:** Periodic oversight — detects drift from the original requirement, identifies concerns, and may reprioritize stories.

**When active:** Periodically during execution

**Supervisor checks:**
1. Are stories progressing toward the original requirement?
2. Is any story drifting from the intended goal?
3. Should any stories be reprioritized?
4. Are there concerns about the overall approach?

**Model recommendation:** Sonnet — good reasoning at reasonable cost.

## Complexity Routing

Stories are routed to roles based on their Fibonacci complexity score:

```
Score  1 ─┐
Score  2 ─┤ Junior (junior_max_complexity: 3)
Score  3 ─┘
Score  4 ─┐
Score  5 ─┘ Intermediate (intermediate_max_complexity: 5)
Score  6 ─┐
Score  8 ─┤ Senior (everything above intermediate threshold)
Score 13 ─┘
```

Thresholds are configurable in `routing`:

```yaml
routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
```

## Reputation Scoring

VXD tracks agent performance across assignments using a weighted scoring system:

| Metric | Weight | Range | Description |
|--------|--------|-------|-------------|
| Quality | 50% | 1-5 | Code quality of completed stories |
| Reliability | 30% | 1-5 | Completion rate without escalation |
| Speed | 20% | 1-5 | Relative completion time |

**Overall Score** = (Quality × 0.5) + (Reliability × 0.3) + (Speed × 0.2)

Scores are stored per-agent in the `agent_scores` SQLite table. Over time, this data can inform routing decisions — agents with higher reputation scores may be preferred for critical stories.

## Escalation Flow

When an agent fails repeatedly, VXD escalates to a higher-tier agent:

```
Junior fails (×2)  ──► Escalate to Intermediate
Intermediate fails (×2)  ──► Escalate to Senior
Senior fails (×2)  ──► ESCALATION_CREATED event (manual intervention needed)
```

Escalations are tracked via `ESCALATION_CREATED` and `ESCALATION_RESOLVED` events and are visible in:
- `vxd escalations` command
- Dashboard Escalations panel

## Prompt Context

Every agent receives a system prompt with these substituted values:

| Placeholder | Source |
|-------------|--------|
| `{team_name}` | Config or default |
| `{repo_path}` | Current repository path |
| `{tech_stack}` | Auto-detected from repo (Go, Node, Python, Rust, etc.) |
| `{story_id}` | Assigned story ID |
| `{story_title}` | Story title from planning |
| `{story_description}` | Full story description |
| `{acceptance_criteria}` | Story acceptance criteria |
| `{lint_command}` | From config or auto-detected |
| `{build_command}` | From config or auto-detected |
| `{test_command}` | From config or auto-detected |

The tech stack is auto-detected by scanning for marker files (`go.mod`, `package.json`, `Cargo.toml`, `pom.xml`, etc.).
