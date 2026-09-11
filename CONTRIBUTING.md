# Contributing to VXD

VXD is the public, Apache-2.0 orchestration core maintained by Vortex Dispatch.
Before proposing a feature, read [`docs/OPEN_CORE.md`](docs/OPEN_CORE.md). The
short version: improve VXD as a strong standalone open-source project, but do
not use this repository to mirror private commercial software-factory IP.

## Branch policy

All changes to `main` should go through a pull request.

```text
main
  ├── feat/my-feature
  ├── fix/broken-thing
  └── docs/update-readme
```

### Workflow

1. Create a branch from `main`.
2. Make the smallest coherent change.
3. Run the relevant build and tests.
4. Push the branch and open a PR.
5. Squash-merge once CI passes.

```bash
git checkout main && git pull origin main
git checkout -b feat/my-feature

go build -o ~/.local/bin/vxd ./cmd/vxd
go test ./... -count=1

git push -u origin feat/my-feature
gh pr create --base main --fill
```

## Branch naming

| Prefix | Use |
|---|---|
| `feat/` | New features |
| `fix/` | Bug fixes |
| `docs/` | Documentation only |
| `test/` | Test additions or fixes |
| `refactor/` | Behaviour-preserving restructuring |
| `chore/` | Build, CI, dependencies and housekeeping |

## Commit messages

Use Conventional Commits where practical:

```text
feat(engine): add bounded retry state
fix(runtime): preserve cancellation on adapter failure
docs: clarify review modes
```

If an AI agent materially contributed to the change, disclose that in the
commit/PR according to your normal contribution practice.

## Testing requirements

- Add tests for behavioural changes.
- Agent-behaviour changes need wiring/integration coverage proving the new path
  is actually activated.
- Event changes need projector and replay coverage.
- Use temporary directories in tests rather than machine-specific paths.
- Run `go test ./... -count=1` before opening a PR unless the affected area
  documents a stricter command.

## Public/private boundary

Good candidates for VXD include:

- provider/runtime adapters,
- CLI and developer-experience improvements,
- worktree and Git reliability,
- generic planning/orchestration improvements,
- basic review and QA,
- dashboards and diagnostics,
- portable event-sourcing/recovery fixes,
- documentation and examples.

Do **not** commit:

- private Vortex Dispatch repository names or paths,
- customer identifiers or customer-specific policies,
- unpublished commercial prompts,
- proprietary benchmark datasets,
- private cross-project learning data,
- production-only routing/optimisation intelligence whose primary purpose is
  the commercial software factory,
- internal product strategy or unpublished roadmaps.

Public and private systems are not required to stay feature-identical. A useful
idea may be independently implemented in VXD when it makes VXD better for its
users, but private code should never be copied here simply to keep parity.

## Event-sourcing rules

The event history is the source of truth; SQLite is a materialized projection.
New state-changing event types must be handled explicitly by the projector and
covered by tests. Replay must remain deterministic.

## Code style

- Prefer pure logic with thin I/O adapters.
- Wrap errors with useful context.
- Preserve cancellation and timeouts across process boundaries.
- Avoid secrets in logs, events and test fixtures.
- Keep runtime/provider-specific behaviour behind interfaces where practical.

## Documentation

Behavioural changes should update user-facing documentation in the same PR.
Public docs should explain observable VXD behaviour, not private Vortex
Dispatch implementation details.
