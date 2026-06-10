---
tags: [moc, index]
---

# VXD MOC — Map of Content

The single entry point to the VXD knowledge vault. VXD orchestrates AI coding
agents (Claude Code, Codex, Gemini CLI) to autonomously implement software
requirements: it decomposes requirements into stories, dispatches agents in
parallel via tmux, monitors progress, runs QA, and merges PRs — with a 5-tier
escalation chain for failures.

## Start here
- [[What is VXD]] — the one-page mental model
- [[Pipeline Flow]] — requirement → stories → agents → QA → merge
- [[Glossary]] — terms used across the vault

## Architecture
- [[Architecture Overview]] — packages and responsibilities
- [[Event Sourcing]] — the append-only log and SQLite projection
- [[Escalation Chain]] — the 5-tier failure-recovery ladder
- [[Conflict Resolution]] — binary/text conflict handling during rebase
- [[Runtime and Adapters]] — how agent commands are built and run

## Operations
- [[Operations Runbook]] — day-to-day commands and recovery
- [[CLI Commands]] — the full command surface
- [[Configuration]] — `vxd.yaml` reference
- [[Debugging Checklist]] — symptom → cause → fix

## Security
- [[Security Model]] — trust boundaries and prompt-injection defenses
- [[Dashboard Authentication]] — the web dashboard token handshake
- [[Security Audit 2026-06]] — findings, fixes, and follow-ups

## Quality
- [[Testing Conventions]] — TDD, wiring tests, hermetic tests
- [[Documentation Enforcement]] — how docs are kept in sync with code

## Related
- Repo docs: `README.md`, `CLAUDE.md`, `docs/architecture-overview.md`
- Audit report: `docs/audits/2026-06-10-security-audit-and-remediation.md`
