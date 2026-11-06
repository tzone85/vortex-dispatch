# Public audit record — July 2026

This is a historical engineering record, not a current certification of VXD.
The live test suite and current documentation determine supported behavior.

## Retained regression coverage

The audit led to structural checks in `test/audit_structural_test.go` for
preflight coverage, public contributor documentation, notification wiring,
health endpoint documentation and event projection behavior.

T-04 — Resolved: autoresearch evolution and merge execution have wiring tests;
public documentation must not describe those implemented paths as placeholders.
See `docs/autoresearch-harness-design.md` and the tests in `internal/autoresearch`.

## Current references

- [Public architecture overview](architecture-overview.md)
- [Package map and event sourcing](architecture.md)
- [Monitoring and recovery](monitoring.md)
- [Publication boundary](OPEN_CORE.md)

Old line-number inventories, cross-product parity instructions and business
planning material have been removed from this public summary. Earlier commits
are historical records; this edit does not remove them from Git history.
