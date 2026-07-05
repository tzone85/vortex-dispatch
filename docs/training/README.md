# VXD Training Library

These guides take you from zero to autonomous expert, including dedicated pathways for using VXD to ship software *and* marketing products with equal ease.

## Training Tracks

### Software Development Track
- [Autonomous Software Development](autonomous-software-development.md) — end-to-end how VXD turns a sentence into merged PRs. Includes escalation, devdb, QA, and resume.

### Product & Marketing Track
- [Product & Marketing as Easy as ABC](product-marketing-made-easy.md) — turn ideas into live sites, landing pages, campaign tools, copy generators, and analytics UIs using the same orchestration engine. VXD + agents = 1-command product launches.

### Extension Track
- Extending VXD, adding custom roles, new metrics for autoresearch, wiring new notifiers.

## Quick Reference Recipes

**Classic dev:**
```
vxd init
vxd preflight
vxd req "Implement JWT auth for the user service with tests and docs"
```

**Marketing site (breeze):**
```
vxd req "Build a beautiful Next.js marketing site for 'Vortex' with hero, 5 feature cards, pricing, testimonials, and a lead-capture form. Deployable static."
```

**Figma → shipped:**
```
vxd figma auth
vxd req "Exactly implement the marketing landing page from this Figma: https://figma.com/..."
```

All use the mature subsystems: event sourcing, 5-tier escalation, security gate, ephemeral DBs when needed, autoresearch for tuning your own agents.

See also top-level tutorial.md and workflows.md.

## Doc Hygiene

Training and knowledge docs are kept current by a mechanical test (`TestAudit_DocsNoStaleAutoresearchStubRefs`). It walks training/, knowledge/autoresearch/, and audit-findings and fails on present-tense references to known stubs for wired subsystems (e.g. evolve/MergePR). T-04 in the audit snapshot is marked resolved. Run `go test ./test -run TestAudit_DocsNoStaleAutoresearchStubRefs` as part of release hygiene. (Last hygiene: 2026-07-06)
tiny doc hygiene note
