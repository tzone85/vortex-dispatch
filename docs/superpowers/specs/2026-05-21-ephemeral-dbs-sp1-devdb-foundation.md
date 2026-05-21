# SP1 — `internal/devdb` Foundation

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Status:** Draft
**Scope:** Provider interface, lifecycle contract, config plumbing, null implementation, event integration. **No real provider** (Ghost / Docker land in SP2 / SP3).

## Purpose

Land the contract every downstream sub-project depends on. Nothing in this PR provisions a real database. We build:

1. The Provider interface and supporting types.
2. The `null.Provider` (no-op) so the rest of the codebase can integrate without touching infra.
3. Config struct + validation.
4. New event types + projection wiring.
5. Lifecycle helper (`devdb.Lifecycle`) that engine code calls — agnostic to backend.

SP4 (executor wiring) cannot land until SP1 lands.

## Package layout

```
internal/devdb/
├── provider.go        # Provider interface + DB struct + errors
├── lifecycle.go       # Lifecycle helper used by engine
├── null/
│   └── null.go        # null.Provider — returns deterministic fake DB
├── envfile.go         # Render .vxd-db/connect.env + README.md + psql.sh
├── naming.go          # vxd-<project>-<story-id-short> formatter
├── recovery.go        # Orphan-recovery helper (used on resume)
├── provider_test.go
├── lifecycle_test.go
├── envfile_test.go
├── naming_test.go
└── recovery_test.go
```

## Public API

### `Provider` interface

```go
package devdb

import "context"

// Provider provisions ephemeral databases for stories.
// Implementations: ghost (SP2), docker (SP3), null (this SP).
type Provider interface {
    // Name returns the provider identifier ("ghost", "docker", "null").
    Name() string

    // Create provisions a new empty database.
    Create(ctx context.Context, opts CreateOpts) (DB, error)

    // Fork creates a copy of a template database.
    // Templates are how we get prod-like data into the per-story DB without
    // copying it on every story.
    Fork(ctx context.Context, template string, opts CreateOpts) (DB, error)

    // Delete removes a database permanently.
    Delete(ctx context.Context, dbID string) error

    // List returns all DBs managed by this provider in the current space/host.
    // Used by orphan recovery on vxd resume.
    List(ctx context.Context) ([]DB, error)

    // Schema returns an agent-friendly text dump of the DB's schema.
    Schema(ctx context.Context, dbID string) (string, error)

    // Ping verifies the provider is reachable. Used by preflight.
    Ping(ctx context.Context) error
}

type CreateOpts struct {
    Name       string            // required; must satisfy naming.IsValid
    Labels     map[string]string // provider-specific; e.g. story_id, requirement_id
    ReadOnly   bool              // when true, ConnectionString returns a read-only DSN
    WaitReady  bool              // block until DB accepts connections
    WaitTimeout time.Duration    // when WaitReady, fail after this
}

type DB struct {
    ID               string            // provider-opaque ID
    Name             string            // canonical name (matches CreateOpts.Name)
    Provider         string            // populated by lifecycle, mirrors Provider.Name()
    ConnectionString string            // postgres://user:pass@host:port/db?sslmode=...
    ReadOnlyDSN      string            // read-only variant if available
    CreatedAt        time.Time
    Labels           map[string]string
}
```

### Errors

```go
var (
    ErrNotFound      = errors.New("devdb: database not found")
    ErrAlreadyExists = errors.New("devdb: database already exists")
    ErrProviderDown  = errors.New("devdb: provider unreachable")
    ErrInvalidName   = errors.New("devdb: invalid database name")
    ErrTemplateMiss  = errors.New("devdb: template database not found")
    ErrUnsupported   = errors.New("devdb: operation not supported by provider")
)
```

Provider implementations wrap underlying errors using `fmt.Errorf("...: %w", ErrXxx)` so callers can `errors.Is`.

### `Lifecycle` helper

```go
// Lifecycle orchestrates provider calls + event emission + envfile writing.
// Engine code uses Lifecycle, not Provider directly.
type Lifecycle struct {
    provider Provider
    events   state.EventStore
    cfg      Config
    clock    func() time.Time // injectable for tests
}

func NewLifecycle(p Provider, es state.EventStore, cfg Config) *Lifecycle

// Provision forks a DB from cfg.Template (or creates empty if Template == "").
// Writes .vxd-db/ files into worktreeDir. Emits STORY_DB_CREATED.
func (l *Lifecycle) Provision(ctx context.Context, storyID, project, worktreeDir string) (DB, error)

// Release deletes the DB and emits STORY_DB_DELETED.
// Honors cfg.OnFailure.KeepDB: if the story failed and KeepDB is true, skip deletion
// and emit STORY_DB_DELETED with status="retained".
func (l *Lifecycle) Release(ctx context.Context, db DB, storyOutcome StoryOutcome) error

type StoryOutcome int
const (
    OutcomeSuccess StoryOutcome = iota
    OutcomeFailed
    OutcomePaused
)
```

### Config

`config.Config` (in `internal/config/config.go`) gains:

```go
type DevDBConfig struct {
    Provider  string             `yaml:"provider"`           // ghost | docker | null
    Template  string             `yaml:"template"`           // source DB name for forks
    OnFailure DevDBFailurePolicy `yaml:"on_failure"`
    Ghost     DevDBGhostConfig   `yaml:"ghost"`
    Docker    DevDBDockerConfig  `yaml:"docker"`
}

type DevDBFailurePolicy struct {
    KeepDB       bool          `yaml:"keep_db"`        // default false
    RetainHours  time.Duration `yaml:"retain_hours"`   // default 24h, used by GC
}

type DevDBGhostConfig struct {
    APIKeyEnv string `yaml:"api_key_env"` // env var name; default GHOST_API_KEY
    SpaceID   string `yaml:"space_id"`    // optional override
}

type DevDBDockerConfig struct {
    Image           string `yaml:"image"`            // default postgres:16
    TemplateVolume  string `yaml:"template_volume"`  // host path for template volumes
    Network         string `yaml:"network"`          // docker network name
    HostPortRange   string `yaml:"host_port_range"`  // "5500-5599" — port to publish on host
}
```

Added to `Config`:
```go
type Config struct {
    // ... existing fields ...
    DevDB DevDBConfig `yaml:"devdb"`
}
```

#### Config validation

`config.Validate()` gains:

```go
func validateDevDB(c DevDBConfig) error {
    switch c.Provider {
    case "", "null":
        return nil // disabled
    case "ghost":
        if c.Template == "" { return fmt.Errorf("devdb.template required for ghost provider") }
        if c.Ghost.APIKeyEnv == "" { c.Ghost.APIKeyEnv = "GHOST_API_KEY" }
        return nil
    case "docker":
        if c.Template == "" { return fmt.Errorf("devdb.template required for docker provider") }
        if c.Docker.Image == "" { c.Docker.Image = "postgres:16" }
        return nil
    default:
        return fmt.Errorf("devdb.provider must be ghost|docker|null, got %q", c.Provider)
    }
}
```

`TestDocCoverage_ConfigSections` (existing) will require `DevDBConfig` to appear in `README.md` Configuration table. SP6 adds the README entries.

### Event types

`internal/state/events.go` gains:

```go
const (
    // ... existing ...
    EventStoryDBCreated EventType = "STORY_DB_CREATED"
    EventStoryDBFailed  EventType = "STORY_DB_FAILED"
    EventStoryDBDeleted EventType = "STORY_DB_DELETED"
)
```

Each event's `Data` JSON payload:

```jsonc
// STORY_DB_CREATED
{
  "db_id": "abc123",
  "db_name": "vxd-mukuru-api-a8cbef1f-3a",
  "provider": "ghost",
  "template": "mukuru-prod-snapshot",
  "conn_string_hash": "sha256:..."  // not the raw DSN
}

// STORY_DB_FAILED
{
  "db_name": "vxd-mukuru-api-a8cbef1f-3a",
  "provider": "ghost",
  "error": "ghost: 503 unavailable",
  "attempt": 2
}

// STORY_DB_DELETED
{
  "db_id": "abc123",
  "duration_seconds": 482.3,
  "bytes_used": 24500000,
  "status": "deleted"  // or "retained" if KeepDB
}
```

### Projection wiring

`internal/state/sqlite.go Project()` gets new cases for the three events. New `story_databases` table:

```sql
CREATE TABLE IF NOT EXISTS story_databases (
    story_id          TEXT NOT NULL,
    db_id             TEXT NOT NULL,
    db_name           TEXT NOT NULL,
    provider          TEXT NOT NULL,
    status            TEXT NOT NULL,   -- created|failed|deleted|retained
    created_at        TIMESTAMP,
    deleted_at        TIMESTAMP,
    duration_seconds  REAL,
    bytes_used        INTEGER,
    PRIMARY KEY (story_id, db_id)
);
```

`SaveSchema()` runs this DDL idempotently.

### `null.Provider`

Returns a deterministic fake DB. Used in tests and as the default no-op.

```go
package null

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "null" }
func (p *Provider) Create(ctx context.Context, opts devdb.CreateOpts) (devdb.DB, error) {
    return devdb.DB{
        ID:               "null-" + opts.Name,
        Name:             opts.Name,
        Provider:         "null",
        ConnectionString: "postgres://null@localhost:0/" + opts.Name,
        CreatedAt:        time.Now(),
        Labels:           opts.Labels,
    }, nil
}
// Fork, Delete, List, Schema, Ping all return zero-value successes.
```

### Envfile writer

```go
// WriteEnvFiles renders .vxd-db/{connect.env, README.md, psql.sh} into worktreeDir.
// File permissions: 0600 for connect.env, 0644 for README.md, 0755 for psql.sh.
func WriteEnvFiles(worktreeDir string, db DB) error
```

`connect.env`:
```
DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=require
DATABASE_URL_READONLY=postgres://...   # if available
DATABASE_PROVIDER=ghost
DATABASE_ID=abc123
DATABASE_NAME=vxd-mukuru-api-a8cbef1f-3a
```

`README.md` (agent-readable):
```markdown
# Your ephemeral database

You have a real Postgres database to yourself. It dies when this story finishes.

- Connection string: see `connect.env`
- Quick connect: `./psql.sh`
- Provider: ghost
- Forked from: mukuru-prod-snapshot

You can:
- Run migrations
- Insert / update / delete data
- Drop tables, create extensions, anything
- Blast radius is this DB only

You cannot:
- Touch production (this is not production)
- Assume the DB persists past this story
```

`psql.sh`:
```sh
#!/usr/bin/env bash
set -eu
source "$(dirname "$0")/connect.env"
exec psql "$DATABASE_URL" "$@"
```

### Naming

```go
package naming

// FormatDBName produces vxd-<project>-<story-id>.
// Project name is lowercased, non-alphanumeric replaced with -, truncated to 32 chars.
// Story ID is used in full (matches VXD's existing reqID-prefixed format,
// e.g. "a8cbef1f-3a" — 8 chars of reqID + dash + LLM-ID).
// Total max length: 63 chars (Postgres identifier limit); we truncate the
// project segment if it would exceed.
func FormatDBName(prefix, project, storyID string) string

// IsValid returns whether name matches /^[a-z][a-z0-9-]{0,62}$/.
func IsValid(name string) bool

// ParseStoryID extracts the story-id portion from a formatted name.
// Returns "" if name doesn't match the format.
func ParseStoryID(prefix, name string) string
```

`prefix` is `"vxd"` in VXD, `"nxd"` in NXD.

### Recovery

```go
// FindOrphans returns DBs whose names match the prefix but are not associated
// with any currently-running story. Used on `vxd resume`.
func FindOrphans(ctx context.Context, p Provider, prefix string, activeStoryIDs []string) ([]DB, error)

// ReleaseOrphans deletes orphan DBs older than minAge.
// Younger orphans are returned for human review.
func ReleaseOrphans(ctx context.Context, p Provider, orphans []DB, minAge time.Duration) (deleted, kept []DB, err error)
```

## Tests (Wave 1 for SP1)

| Test | Asserts |
|------|---------|
| `TestProviderInterface_Compiles` | `null.Provider` satisfies `devdb.Provider` |
| `TestNullProvider_RoundTrip` | Create → Fork → List → Delete deterministic |
| `TestLifecycle_Provision_EmitsEvent` | `STORY_DB_CREATED` written to event store |
| `TestLifecycle_Provision_WritesEnvFiles` | `.vxd-db/connect.env` present, mode 0600 |
| `TestLifecycle_Release_EmitsDeleted_OnSuccess` | `STORY_DB_DELETED` status=deleted |
| `TestLifecycle_Release_KeepsDB_OnFailure_WhenConfigured` | `KeepDB=true` → status=retained, no Delete call |
| `TestLifecycle_Release_DeletesDB_OnFailure_WhenNotConfigured` | `KeepDB=false` → status=deleted |
| `TestEnvfile_Render_AllFiles` | 3 files written, contents match goldens |
| `TestNaming_Format_RespectsLimit` | 63-char Postgres limit honored |
| `TestNaming_IsValid_*` | Table-driven valid/invalid cases |
| `TestNaming_ParseStoryID_Roundtrip` | `ParseStoryID(FormatDBName(x)) == x` |
| `TestRecovery_FindOrphans` | Returns only non-active prefix-matching DBs |
| `TestRecovery_ReleaseOrphans_AgeFilter` | Deletes ≥minAge, keeps younger |
| `TestConfigValidate_DevDB_Provider` | Table-driven valid/invalid configs |
| `TestConfigValidate_DevDB_Defaults` | Missing fields get defaults |
| **Wiring**: `TestWiring_StoryDBCreated_UpdatesProjection` | `story_databases` row written |
| **Wiring**: `TestWiring_StoryDBDeleted_UpdatesProjection` | `status` and `deleted_at` updated |
| **Wiring**: `TestWiring_StoryDBFailed_UpdatesProjection` | Failed row inserted/updated |
| **Wiring**: `TestDocCoverage_ConfigSections_DevDB` | `DevDBConfig` appears in README |

All run via `go test ./internal/devdb/... ./internal/state/... ./internal/engine/... -count=1`.

Coverage target: ≥85% for `internal/devdb`.

## Non-test acceptance

- `go build -o ~/.local/bin/vxd ./cmd/vxd` succeeds.
- `vxd config show` displays the new `devdb` section with defaults.
- `vxd config validate` rejects an invalid provider value.
- No new external dependencies — Go stdlib only for SP1.

## What is explicitly NOT in SP1

- Any HTTP client (ghost). That's SP2.
- Any Docker client. That's SP3.
- Executor / spawn wiring. That's SP4.
- QA criteria. That's SP5.
- CLI / dashboard. That's SP6.

## Open questions deferred to impl

- Should `Lifecycle` accept a `context.Context` only or also a deadline? Lean toward context-only; deadline comes from cfg.
- `Provider.Schema()` output format — text now, MCP-friendly JSON later. SP6 may switch to JSON.
