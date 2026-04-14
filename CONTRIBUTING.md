# Contributing to VXD

## Branch Policy

**All changes to `main` must go through a pull request.** Direct pushes to `main` are not allowed by convention.

```
main (protected by convention)
  └── feat/my-feature   ← work here
  └── fix/broken-thing  ← or here
  └── docs/update-readme ← or here
```

### Workflow

1. **Create a feature branch** from `main`:
   ```bash
   git checkout main && git pull origin main
   git checkout -b feat/my-feature
   ```

2. **Make your changes**, commit with conventional messages (see below).

3. **Run tests** before pushing:
   ```bash
   go build ./cmd/vxd/
   go test $(go list ./... | grep -v improve) -count=1
   go test -tags e2e ./test/   # optional: E2E tests
   ```

4. **Push and open a PR**:
   ```bash
   git push -u origin feat/my-feature
   gh pr create --base main --fill
   ```

5. **Squash-merge** the PR once CI passes:
   ```bash
   gh pr merge --squash --delete-branch
   ```

6. **Clean up locally**:
   ```bash
   git checkout main && git pull origin main
   git branch -d feat/my-feature
   ```

### Branch Naming

| Prefix | Use |
|--------|-----|
| `feat/` | New features |
| `fix/` | Bug fixes |
| `docs/` | Documentation only |
| `test/` | Test additions or fixes |
| `refactor/` | Code restructuring (no behavior change) |
| `chore/` | Build, CI, dependency updates |

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short description

Longer explanation if needed.

Co-Authored-By: Oz <oz-agent@warp.dev>
```

**Types**: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `perf`, `ci`

**Scope** (optional): the package or area, e.g., `engine`, `repolearn`, `cli`

**Co-author line**: Always include when working with an AI agent.

### Examples

```
feat(repolearn): add Pass 2 git history analysis
fix(engine): use prefixed story IDs in E2E test
docs: add Repo Learning section to README
test(engine): add wiring tests for profile injection
chore(ci): update Go version to 1.26
```

## Testing Requirements

- **TDD is mandatory** — write tests before or alongside implementation.
- **Wiring tests** (`engine/wiring_test.go`) are required for any feature that modifies agent behavior. These prove the feature is *activated*, not just implemented.
- **All tests must pass** before opening a PR:
  ```bash
  go test $(go list ./... | grep -v improve) -count=1
  ```
- The `internal/improve/` package is excluded from the default test run because it contains a flaky prompt injection test that requires network access.
- E2E tests use the `e2e` build tag:
  ```bash
  go test -tags e2e ./test/
  ```

## Build

```bash
# Build to the standard location (CRITICAL: not ~/go/bin/)
go build -o ~/.local/bin/vxd ./cmd/vxd

# Or use make
make build
make test
make lint
```

## Event Sourcing Rules

- New event types **MUST** be handled in `sqlite.go Project()` — the `default` case silently ignores unknown events.
- Always add a wiring test when introducing a new event type.
- Events are the source of truth; SQLite projections are materialized views.

## Code Style

- Follow existing patterns — `package engine` (internal tests) preferred over `engine_test`.
- Pure functions for logic, thin adapters for I/O.
- Use `t.TempDir()` in tests, never hardcoded paths.
- Error wrapping: `fmt.Errorf("context: %w", err)`.

## VXD vs NXD

VXD (private, cloud APIs) and NXD (public, Ollama) share core packages. When making changes:

- **NEVER** reference VXD in NXD code.
- Core fixes should be ported to NXD.
- Module path: `github.com/tzone85/vortex-dispatch` (VXD) vs `github.com/tzone85/nexus-dispatch` (NXD).
