# SP5 — QA Migration Gate

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Depends on:** SP1, SP4
**Status:** Draft
**Scope:** New declarative criteria that exercise the story's ephemeral DB, plus optional pre-merge fresh-fork verification.

## Purpose

The per-story DB is useful by itself (agents can experiment in isolation). But the killer use case is the QA gate: before a story merges, take a fresh fork of the template, apply the story's migration changes against it, and run tests. If it passes on a clean prod-like DB, the migration is safe to merge.

This SP adds:

1. New `CriterionKind`s: `migration_succeeds`, `schema_changed`, `sql_query_returns`.
2. A `qa.fresh_fork_verification` mode that re-forks before running QA.
3. Integration with existing `engine/qa.go` and `engine/criteria.go`.

## New criterion kinds

`internal/engine/criteria.go` gains:

```go
const (
    // ... existing ...
    KindMigrationSucceeds CriterionKind = "migration_succeeds"
    KindSchemaChanged     CriterionKind = "schema_changed"
    KindSQLQueryReturns   CriterionKind = "sql_query_returns"
)

type Criterion struct {
    Kind          CriterionKind     `yaml:"kind"`
    Value         string            `yaml:"value,omitempty"`
    Path          string            `yaml:"path,omitempty"`
    // NEW:
    Command       string            `yaml:"command,omitempty"`   // for migration_succeeds: command that runs the migration tool
    SQL           string            `yaml:"sql,omitempty"`       // for sql_query_returns
    ExpectedRows  *int              `yaml:"expected_rows,omitempty"` // optional row-count assertion
    SchemaBaseline string           `yaml:"schema_baseline,omitempty"` // for schema_changed: optional baseline file path
}
```

### `migration_succeeds`

Runs the project's migration command (`alembic upgrade head`, `npx prisma migrate deploy`, `golang-migrate up`, etc.) against the story's DB. The command is provided by the project; we don't try to auto-detect.

```yaml
qa:
  success_criteria:
    - kind: migration_succeeds
      command: "alembic upgrade head"
```

Evaluator:
1. Read `DATABASE_URL` from `.vxd-db/connect.env`.
2. `cd <worktree> && DATABASE_URL=... <command>`.
3. Exit 0 → pass. Non-zero → fail with captured stderr.

### `schema_changed`

Asserts that the story's work produced *some* schema change (catches stories that claim to add migrations but don't). Optional baseline file comparison.

```yaml
qa:
  success_criteria:
    - kind: schema_changed
      schema_baseline: ".vxd-db/baseline-schema.txt"  # written at story start
```

Implementation:
1. At story start (SP4 lifecycle), if any criterion of `kind: schema_changed` exists, dump schema to `.vxd-db/baseline-schema.txt`.
2. At QA time, dump current schema and diff. Non-empty diff → pass.
3. If `schema_baseline` is set and points outside `.vxd-db/`, use that as comparison (allows project to specify "this should match prod schema after migrations").

### `sql_query_returns`

Run an arbitrary query, assert it returns rows (or a specific row count). Used for data-integrity checks ("user table should have at least one row after migration").

```yaml
qa:
  success_criteria:
    - kind: sql_query_returns
      sql: "SELECT 1 FROM information_schema.tables WHERE table_name = 'users'"
      expected_rows: 1
```

Evaluator: connect via `DATABASE_URL`, run query, check `rows.Next()` count.

## Fresh-fork verification (optional, opt-in)

For maximum confidence, projects can opt into "fresh fork verification": before QA runs, the story's DB is *replaced* with a fresh fork from the template. The story's migrations are then re-applied (via `migration_succeeds` criterion). This catches bugs where the agent's mid-flight DB state masked a missing migration.

```yaml
qa:
  fresh_fork_verification: true
  success_criteria:
    - kind: migration_succeeds
      command: "alembic upgrade head"
    - kind: command_succeeds
      command: "pytest tests/integration"
```

Implementation in `engine/qa.go`:

```go
func (q *QA) Run(ctx context.Context, story PlannedStory, worktree string, db devdb.DB) Result {
    if q.cfg.FreshForkVerification && q.lifecycle != nil {
        // Delete the story's DB, fork a fresh one, rewrite .vxd-db/connect.env.
        freshDB, err := q.lifecycle.RefreshStoryDB(ctx, story.ID, q.projectName, worktree)
        if err != nil {
            return Result{Pass: false, Reason: "fresh fork failed: " + err.Error()}
        }
        db = freshDB
    }

    // Existing criterion loop, now with DSN context.
    return q.evaluateAll(ctx, story, worktree, db)
}
```

`Lifecycle.RefreshStoryDB` is a new SP1-level helper added by SP5:

```go
// RefreshStoryDB deletes the existing DB associated with storyID, forks a fresh
// one from the template, and rewrites worktree/.vxd-db/connect.env.
// Emits STORY_DB_DELETED (status: refreshed) + STORY_DB_CREATED.
func (l *Lifecycle) RefreshStoryDB(ctx context.Context, storyID, project, worktreeDir string) (DB, error)
```

## Config addition

```yaml
qa:
  fresh_fork_verification: false  # default
  success_criteria:
    - { kind: migration_succeeds, command: "..." }
    - { kind: sql_query_returns,  sql: "...", expected_rows: 1 }
```

Validation:
- `migration_succeeds` requires `command`.
- `sql_query_returns` requires `sql`.
- `schema_changed` has no required fields.
- `fresh_fork_verification: true` requires `devdb.provider != null` (otherwise QA fails fast with explanatory error).

## Tests (Wave 1 for SP5)

Unit (no DB needed — use `null.Provider` + fake QA-command runner):

| Test | Asserts |
|------|---------|
| `TestCriterion_MigrationSucceeds_Pass` | exit 0 → pass |
| `TestCriterion_MigrationSucceeds_Fail` | exit 1 → fail; stderr captured |
| `TestCriterion_SchemaChanged_HasDiff` | non-empty diff → pass |
| `TestCriterion_SchemaChanged_NoDiff` | empty diff → fail |
| `TestCriterion_SQLQueryReturns_Match` | expected rows hit → pass |
| `TestCriterion_SQLQueryReturns_Miss` | row count off → fail |
| `TestQA_FreshForkVerification_Refreshes` | RefreshStoryDB called when flag true |
| `TestQA_FreshForkVerification_Skipped_NoDevDB` | flag true but provider null → fast fail |
| `TestConfigValidate_QA_FreshForkRequiresDevDB` | provider=null + flag=true → validation error |

Integration (`-tags=integration`, real Docker via SP3):

| Test | Asserts |
|------|---------|
| `TestIntegration_QA_MigrationSucceeds_RealMigration` | apply a real Alembic-style migration, schema reflects it |
| `TestIntegration_QA_FreshFork_DropsStoryChanges` | story made data writes; fresh fork wipes them, migration re-applied cleanly |

## Wave 2/3 acceptance scenarios

For Wave 2 (VXD + Ghost) and Wave 3 (NXD + Docker):

- Project: a small Postgres test fixture with `migrations/` directory.
- Submit `vxd req "Add 'users.email' column"`.
- Story dispatches → agent creates migration file.
- QA runs with `migration_succeeds` criterion.
- PR merges only when migration applies cleanly on fresh fork.
- Verify `STORY_DB_DELETED` events show `status: deleted` (success) and `status: retained` (intentional failure scenario for postmortem).

## Open questions

- Multi-migration stories: if a single story adds 5 migration files, should we run them one-by-one and snapshot between? Phase-2; current QA runs the whole `migrate up` once.
- Down-migration verification — `migrate down → migrate up` ratchet test? Phase-2 candidate. Worth a separate spec when prioritized.
- Migration tool detection — we don't auto-detect. Project's `vxd.yaml` must declare the command. Documented in SP6's use-case doc.
