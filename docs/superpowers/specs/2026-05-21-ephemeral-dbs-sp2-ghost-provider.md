# SP2 — Ghost Provider (VXD only)

**Parent:** `2026-05-21-ephemeral-dbs-master-design.md`
**Depends on:** SP1
**Status:** Draft
**Scope:** Implement `devdb.Provider` for ghost.build's HTTP API.

## Purpose

Provide the real provider for VXD when running against ghost.build's cloud service. NXD does not get this.

## Package layout

```
internal/devdb/ghost/
├── provider.go        # ghost.Provider implements devdb.Provider
├── client.go          # thin HTTP client for api.ghost.build/v0
├── auth.go            # headless device-flow OAuth helper (one-shot)
├── errors.go          # maps Ghost HTTP errors to devdb sentinels
├── provider_test.go   # uses httptest.Server
├── client_test.go
└── auth_test.go
```

## External surface

```go
package ghost

type Provider struct {
    client *Client
    cfg    Config
    clock  func() time.Time
}

type Config struct {
    APIKey   string        // resolved from env (DevDBGhostConfig.APIKeyEnv)
    SpaceID  string        // optional; client resolves first space if empty
    BaseURL  string        // default "https://api.ghost.build/v0"
    Timeout  time.Duration // default 30s
    UserAgent string       // default "vxd/<version>"
}

func New(cfg Config) (*Provider, error) // validates API key shape
```

## HTTP client (thin wrapper, no SDK)

```go
type Client struct {
    httpClient *http.Client
    baseURL    string
    apiKey     string
    userAgent  string
}

// Endpoints used (from docs scrape — api.ghost.build/v0):
//   GET    /spaces
//   GET    /spaces/{space_id}/databases
//   POST   /spaces/{space_id}/databases                            (create)
//   POST   /spaces/{space_id}/databases/{db_ref}/fork              (fork)
//   DELETE /spaces/{space_id}/databases/{db_ref}                   (delete)
//   GET    /spaces/{space_id}/databases/{db_ref}                   (get)
//   POST   /spaces/{space_id}/databases/{db_ref}/password          (rotate; not used by SP2)
//   POST   /spaces/{space_id}/databases/{db_ref}/pause
//   POST   /spaces/{space_id}/databases/{db_ref}/resume
//   GET    /spaces/{space_id}/databases/{db_ref}/logs
//   GET    /health
```

Authentication: `Authorization: Bearer <api_key>`.

Retry policy: 1 retry on 5xx with 500ms exponential backoff. No retry on 4xx (caller bug).

Rate-limit handling: respect `Retry-After` header on 429.

## `Provider.Create`

1. Resolve space (cached after first call).
2. `POST /spaces/{space}/databases` with body `{"name": opts.Name}`.
3. If `opts.WaitReady`: poll `GET /databases/{id}` until `status == "running"` or timeout.
4. Compose `DB`:
   - `ConnectionString` from response (Ghost returns `dsn` field).
   - `ReadOnlyDSN` from `--read-only` variant via `ghost connect --read-only` semantics — see open question.
   - `Labels`: provider-passed labels not currently supported by Ghost API → store locally only.

## `Provider.Fork`

1. Look up template by name → ID via `GET /spaces/{space}/databases?name=<template>` (Ghost API confirmed to support name lookup).
2. `POST /spaces/{space}/databases/{template_id}/fork` with body `{"name": opts.Name}`.
3. Same wait + compose logic as Create.

If template not found, return `ErrTemplateMiss` (wrapped).

## `Provider.Delete`

1. Resolve `db_ref` from `opts.Name` or DB ID.
2. `DELETE /spaces/{space}/databases/{db_ref}?confirm=true`.
3. 404 → wrap `ErrNotFound`. 200/204 → success.

## `Provider.List`

1. `GET /spaces/{space}/databases`.
2. Map response array → `[]devdb.DB`.
3. Filter happens at SP1 layer (`FindOrphans` matches by prefix).

## `Provider.Schema`

Ghost's API does not directly expose a schema dump endpoint (CLI command `ghost schema` is computed client-side). We replicate by:

1. Get DSN from provider (cached after Create / via GET).
2. Connect via `pgx` / `database/sql` lazily (only when Schema is called).
3. Query `information_schema.tables`, `pg_indexes`, `pg_constraint`, etc.
4. Render to deterministic text (table → columns → constraints → indexes).

Implementation note: keep DB connection pool size = 1 and close after `Schema()` returns. Avoids leaking connections.

Trade-off: SP2 introduces a `pgx` dependency. That's fine — `internal/devdb/ghost` is the only package that needs it. Add `github.com/jackc/pgx/v5` to `go.mod`.

## `Provider.Ping`

`GET /health`. 200 → nil. Anything else → `ErrProviderDown`.

## Auth helper (`auth.go`)

Single function:

```go
// EnsureAPIKey reads cfg.APIKey. If empty, falls back to env (resolveAPIKey).
// If still empty and runMode == "interactive", prompts user to run `ghost login --headless`
// and copies the resulting key from ~/.config/ghost/credentials.json.
func EnsureAPIKey(ctx context.Context, cfg DevDBGhostConfig, runMode string) (string, error)
```

We do **not** implement the GitHub OAuth device flow ourselves; we delegate to the user's `ghost` CLI. Rationale: the `ghost` binary is already required for the MCP install in SP6, so users have it. Implementing OAuth ourselves doubles auth surface area.

## Error mapping

```go
// Maps Ghost HTTP errors to devdb sentinels via errors.Is matching.
func wrapHTTPError(resp *http.Response, body []byte) error {
    switch resp.StatusCode {
    case 401, 403: return fmt.Errorf("ghost auth: %s: %w", body, devdb.ErrProviderDown)
    case 404:      return fmt.Errorf("ghost not found: %w", devdb.ErrNotFound)
    case 409:      return fmt.Errorf("ghost conflict: %w", devdb.ErrAlreadyExists)
    case 429:      return fmt.Errorf("ghost rate limit: %w", devdb.ErrProviderDown)
    case 500, 502, 503, 504: return fmt.Errorf("ghost server: %d %s: %w", resp.StatusCode, body, devdb.ErrProviderDown)
    default:       return fmt.Errorf("ghost: %d %s", resp.StatusCode, body)
    }
}
```

## Tests (Wave 1 for SP2)

All tests use `httptest.NewServer` — no live Ghost calls.

| Test | Asserts |
|------|---------|
| `TestClient_Create_HappyPath` | POST body + auth header + DB parsed |
| `TestClient_Create_429_Retries` | Respects Retry-After |
| `TestClient_Create_500_Retries_Once` | 1 retry, then surfaces error |
| `TestClient_Fork_TemplateNotFound` | Returns `ErrTemplateMiss` |
| `TestClient_Delete_404_ReturnsNotFound` | Wraps `ErrNotFound` |
| `TestClient_List_PaginationHandled` | If Ghost paginates, follow next link |
| `TestProvider_Create_WaitReady_Polls` | Polls until status=running, then returns |
| `TestProvider_Create_WaitReady_Timeout` | Returns deadline error |
| `TestProvider_Schema_TextFormat` | Schema dump matches golden for fixture DB |
| `TestProvider_Ping_Healthy` | 200 → nil |
| `TestProvider_Ping_Unreachable` | 503 → `ErrProviderDown` |
| `TestAuth_EnsureAPIKey_FromEnv` | Reads env var |
| `TestAuth_EnsureAPIKey_FromConfig` | Reads ~/.config/ghost/credentials.json |
| `TestAuth_EnsureAPIKey_Missing` | Returns helpful error with `ghost login --headless` |

Coverage target: ≥80%. Schema-dump test uses a real `pgx` connection to a Docker Postgres — gated by `-tags=integration` so unit tests don't need Docker.

## Wave 2 (live testing — separate PR)

Hits real `api.ghost.build`. Requires `GHOST_API_KEY` and a free-tier account.

```
WAVE_2_GHOST_TESTS=1 GHOST_API_KEY=... go test ./internal/devdb/ghost/... -run TestLive
```

Live tests:
1. Create + delete a unique-named DB.
2. Fork a template (uses a `vxd-test-template` we pre-create) + delete fork.
3. Pause + resume.
4. Logs fetch.
5. List shows expected DBs and only deletes test-prefixed ones (`vxd-test-*`).

Live tests clean up after themselves (defer Delete). Test wraps in t.Cleanup so failures still GC.

## Cost guard

Live tests run only on the `vxd-test-*` namespace. Free tier = 100 compute-hours/mo + 1TB. Each test cycle (create → delete) is <1 minute. 50 cycles ≈ 1 hour. Safe.

CI does **not** run Wave 2 (no secret in CI). Wave 2 is manual / local.

## Open questions deferred to impl

- Does Ghost API return read-only DSN directly, or only via `ghost connect --read-only`? Need to confirm; if not API-exposed, build via `?options=-c default_transaction_read_only=on` query string on the standard DSN.
- Pagination — docs scrape did not show explicit pagination params; verify empirically.
- Template caching — `space_id` is cached, but template ID lookup happens per Fork. Add a small LRU.
