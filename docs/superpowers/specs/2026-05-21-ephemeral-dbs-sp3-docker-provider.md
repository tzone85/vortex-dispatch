# SP3 — Docker Provider (VXD + NXD)

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Depends on:** SP1
**Status:** Draft
**Scope:** Implement `devdb.Provider` against a local Docker daemon. NXD's only provider; VXD's offline fallback.

## Purpose

Ghost is cloud. NXD must stay offline-first. Docker is the universally-available local substrate. This SP delivers an implementation of `devdb.Provider` that:

- Runs Postgres in Docker containers.
- Uses Postgres' built-in `CREATE DATABASE x TEMPLATE y` for sub-second forks.
- Lives entirely on the host — no network calls.

## Package layout

```
internal/devdb/docker/
├── provider.go        # docker.Provider implements devdb.Provider
├── client.go          # thin docker-engine API wrapper (HTTP via unix socket)
├── pg.go              # pgx helpers for CREATE DATABASE TEMPLATE
├── ports.go           # host-port allocator (from cfg.HostPortRange)
├── template.go        # template DB lifecycle (seed, refresh, list)
├── gc.go              # orphan container/volume cleanup
├── provider_test.go
├── client_test.go     # uses httptest replicating docker engine
├── pg_test.go         # integration via testcontainers (-tags=integration)
├── ports_test.go
└── gc_test.go
```

## Design model

A single long-lived Postgres container per host (the "devdb host" container) hosts many databases inside it. Forks are `CREATE DATABASE ... TEMPLATE ...` SQL — sub-second.

Why not one container per DB?
- Container startup is 3–5s; SQL fork is <500ms.
- One pg instance handles 1000s of dbs trivially.
- Lower disk overhead (shared pg processes, shared OS page cache).

When *do* we use multiple containers? Only when the project pins a specific Postgres version that differs from the host pg version. Then we spin up a per-project container (still long-lived).

```
┌──────────────────────────────────────────────┐
│  Host                                         │
│  ┌─────────────────────────────────────────┐ │
│  │ Container: vxd-devdb-pg16                │ │
│  │ image: postgres:16                       │ │
│  │ port: 5432 → host 5500                   │ │
│  │ volume: vxd-devdb-pg16-data              │ │
│  │                                          │ │
│  │ Databases inside:                        │ │
│  │   - mukuru-prod-snapshot (template)      │ │
│  │   - vxd-mukuru-api-a8cbef1f-3a (story)   │ │
│  │   - vxd-mukuru-api-3b8901cd-1f (story)   │ │
│  └─────────────────────────────────────────┘ │
└──────────────────────────────────────────────┘
```

## External surface

```go
package docker

type Provider struct {
    client       *Client       // talks to /var/run/docker.sock
    pg           *pg.Helper    // pgx-backed SQL helper
    cfg          Config
    ports        *Allocator    // host-port allocator
    clock        func() time.Time
}

type Config struct {
    Image          string // default postgres:16
    ContainerName  string // default vxd-devdb-pg16 (or nxd-...)
    TemplateVolume string // host path, default ~/.vxd/devdb-data
    Network        string // default vxd-devdb (or nxd-...)
    HostPortRange  string // "5500-5599"
    AdminUser      string // default postgres
    AdminPassword  string // default: read from ~/.vxd/devdb-admin.pw; auto-generated if missing
    StartTimeout   time.Duration // default 60s
}

func New(cfg Config) (*Provider, error)
```

## Lifecycle within the provider

### Boot
On `New()`:
1. Check Docker daemon reachable (`/_ping`). If not, return `ErrProviderDown`.
2. Check container exists and is running. If not, `EnsureContainer()`:
   - Create network if missing.
   - Create volume if missing.
   - Read or generate admin password (file mode 0600, `~/.vxd/devdb-admin.pw`).
   - `docker run -d --name ... -v ... -p <host>:5432 -e POSTGRES_PASSWORD=... <image>`.
   - Wait for pg readiness (poll `pg_isready` via exec).

### Create
1. Connect as admin via pgx.
2. `CREATE DATABASE "<opts.Name>"`.
3. Compose DSN: `postgres://<user>:<pass>@localhost:<hostPort>/<opts.Name>?sslmode=disable`.
4. If `opts.ReadOnly`: also compose `ReadOnlyDSN` with `?options=-c default_transaction_read_only=on`.
5. Return `DB`.

### Fork
1. Verify template exists: `SELECT 1 FROM pg_database WHERE datname = '<template>'`.
2. **Critical**: template must have no active connections (Postgres limitation). Helper enforces by tagging templates `datistemplate=true`:
   - First time a template is forked from, run `UPDATE pg_database SET datistemplate=true WHERE datname='<template>'`.
   - Templates are read-only by virtue of `datistemplate=true` (Postgres rejects connections).
3. `CREATE DATABASE "<opts.Name>" WITH TEMPLATE "<template>"`.
4. Compose DSN as Create.

### Delete
1. Force-disconnect: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '<name>'`.
2. `DROP DATABASE IF EXISTS "<name>"`.
3. Free the host port from the allocator (relevant if/when we move to per-DB containers).

### List
`SELECT datname, ... FROM pg_database WHERE datname LIKE '<prefix>%' OR datname IN (templates)`.

### Schema
Same logic as Ghost provider — query `information_schema.tables` etc., render to text. Shared helper in `internal/devdb/schema.go` (move from SP2's draft package).

### Ping
- Daemon: `/_ping`.
- Postgres: `pg_isready` via container exec, or simple `SELECT 1`.

## Templates

Templates are first-class. Seeding workflow:

```bash
# Initial seed from a pg_dump
gunzip -c mukuru-prod.dump.gz | vxd db template create mukuru-prod-snapshot --from-stdin

# Refresh weekly
vxd db template refresh mukuru-prod-snapshot --source ~/snapshots/latest.dump.gz
```

CLI lands in SP6. SP3 only owns the provider-side functions:

```go
// CreateTemplate restores a pg_dump (custom format) into a new template DB.
// Sets datistemplate=true on success.
func (p *Provider) CreateTemplate(ctx context.Context, name string, dump io.Reader) error

// RefreshTemplate drops + recreates a template atomically (uses temp name + swap).
func (p *Provider) RefreshTemplate(ctx context.Context, name string, dump io.Reader) error

// ListTemplates returns datname for all rows where datistemplate=true.
func (p *Provider) ListTemplates(ctx context.Context) ([]string, error)
```

Restore implementation: `docker exec` into the container running `pg_restore`. We don't pipe through Go because pg_restore handles custom format binary streams natively.

## Port allocation

When per-DB containers are needed (future / version pinning), `ports.Allocator` hands out host ports from `HostPortRange`:

```go
type Allocator struct { ... }
func NewAllocator(rangeSpec string) (*Allocator, error)  // parses "5500-5599"
func (a *Allocator) Acquire() (int, error)               // returns free port or ErrExhausted
func (a *Allocator) Release(port int)
```

Backed by a `map[int]bool` + bind-and-close check to detect conflicts. SP3 ships the allocator but only uses it for the host container's port (single port).

## GC / orphan recovery

On `vxd resume`, we call:

```go
// CollectOrphans returns DBs (other than templates and known stories) that match the prefix.
func (p *Provider) CollectOrphans(ctx context.Context, prefix string, activeStoryIDs []string) ([]devdb.DB, error)
```

Uses the SP1 `naming.ParseStoryID`. Older-than-`retain_hours` orphans are deleted. Younger ones are reported to the operator.

## Configuration of the host container

We do **not** ship a custom Postgres image. Use plain `postgres:16` (or whatever the project pins). When pgvector / TimescaleDB are needed, the project's `devdb.docker.image` is set to `timescale/timescaledb-ha:pg16-all` and the provider works the same — those extensions are pre-installed in that image.

If a template was created with extensions enabled, every fork inherits them. No extra config.

## Security

- Admin password file: 0600, `~/.vxd/devdb-admin.pw`.
- Per-DB passwords: not used (we use admin password for the host container; isolation is via separate DBs not separate roles).
  - **Mitigation:** the connection string handed to agents is restricted to one DB via the DSN's path. Agents *could* try to connect to admin DB if they get clever, but: (a) they're VXD-spawned, not adversarial; (b) the worktree's `connect.env` only contains the story-DB DSN; (c) for adversarial-agent use cases, future Phase-2 work creates per-DB roles.
- Docker socket access required (host). Surface in preflight.

## Tests (Wave 1 for SP3)

Unit tests (`go test ./internal/devdb/docker/`):

| Test | Asserts |
|------|---------|
| `TestClient_Ping_HappyPath` | httptest mocks `/_ping` → 200 |
| `TestClient_Ping_DaemonDown` | conn-refused → `ErrProviderDown` |
| `TestClient_EnsureContainer_CreatesIfMissing` | inspect → 404 then create → 201 |
| `TestClient_EnsureContainer_StartsIfStopped` | inspect → exited, then `/start` called |
| `TestPorts_Acquire_Release_Cycle` | round-trip |
| `TestPorts_Exhausted` | `ErrExhausted` after range full |
| `TestNaming_ContainerName` | derived from prefix |

Integration tests (`-tags=integration`, require real Docker):

| Test | Asserts |
|------|---------|
| `TestIntegration_Provider_CreateDelete` | DSN connects, DB exists, then doesn't |
| `TestIntegration_Provider_Fork_FromTemplate` | seeded template → fork → row count matches |
| `TestIntegration_Provider_Fork_TemplateBusy` | active connection on template → error surfaces |
| `TestIntegration_Provider_Schema_TextFormat` | matches golden |
| `TestIntegration_Templates_CreateRefreshList` | round-trip |
| `TestIntegration_GC_CollectOrphans` | only non-active match prefix |
| `TestIntegration_GC_ReleaseOrphans_HonorRetention` | only ≥retain_hours deleted |

CI gating:
- Wave 1 unit tests run on every CI build (no Docker needed).
- Integration tests run via `make test-integration` locally and in a separate "integration" CI job (already exists for VXD? — verify; if not, this PR adds it).

Coverage target: ≥80% unit. Integration is acceptance, not coverage.

## Wave 3 (live testing — separate PR, paired with NXD)

Run the full pipeline end-to-end:
1. `vxd req` against a tracked project with `devdb.provider: docker` enabled.
2. Stories spawn → each gets a fork.
3. Story commits migrations → QA gate verifies.
4. Story merges → DB GC'd.
5. Postmortem: `vxd db list` shows zero leaked DBs.

Same script reused for NXD (one binary swap).

## Dependencies

- `github.com/jackc/pgx/v5` (shared with SP2).
- No Docker SDK; we hit `/var/run/docker.sock` directly via `net/http` to keep deps tight.

## Open questions deferred to impl

- Should we use the official Docker SDK (`docker/docker/client`)? Adds ~10 MB to the binary. Decision: start without; revisit if our HTTP wrapper grows past 500 LOC.
- Concurrent fork limit? Postgres serializes `CREATE DATABASE TEMPLATE` per template (template lock). Three stories all forking same template = 3 sequential ops. Probably fine; add `sync.Mutex` per-template if tests show contention.
- pgvector / TimescaleDB image choice — Phase-2 decision, not blocking.
