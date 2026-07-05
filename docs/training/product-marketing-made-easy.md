# Product & Marketing Made as Easy as ABC with VXD

VXD was built for autonomous software engineering — but that same power makes launching *marketing products* trivial: landing pages, campaign microsites, documentation portals, lead-gen tools, email sequence generators, analytics dashboards, ad creative pipelines.

One `vxd req`, walk away, come back to a PR with working, reviewed, merged assets.

## Why VXD for Marketing?

- **Same CLI, same maturity**: full event log, dashboard, resume on crash, escalations if the LLM agent gets the hero section wrong.
- **Real git worktrees + PRs**: your marketing site lives in the repo like any feature. Review diffs, QA (build + visual criteria), security scan (no leaked keys in env examples).
- **Figma native**: `vxd figma` + req gives pixel-perfect implementations from design.
- **Ephemeral everything**: test forms against devdb or mock, no prod impact.
- **Autoresearch your brand voice**: tune the "program.md" for your marketing agent to produce on-brand copy every time.
- **Self-improving**: `vxd improve` can research better CTAs and landing patterns over time.

No separate marketing toolchain. VXD orchestrates Claude/Codex/Gemini to build the *entire product surface*.

## ABC Workflow (Always Be Creating)

### A — Author the brief (1 line or 1 file)
```bash
vxd req "Create a high-converting marketing site for Vortex Dispatch: hero with CTA, 6 feature value props, social proof, pricing tiers (free/pro/enterprise), FAQ, footer. Use Tailwind, ship as Next.js static export. Include realistic demo screenshots."
```

Or write a `marketing-brief.md` and `vxd req --file marketing-brief.md`

### B — Background agents do the work
- Tech Lead decomposes: hero story, features grid, pricing, form integration, copy variants, build/QA.
- Parallel agents in worktrees.
- Review + declarative QA (build succeeds, links valid, no console errors).
- Security gate on any new scripts.
- Squash merge to main.

Dashboard auto-opens: watch live in browser.

### C — Collect & iterate (or ship)
- PR lands.
- `vxd report <id> --html` gives client-facing summary.
- Deploy (Vercel/Netlify hook or manual).
- Want better conversion? Submit follow-up req: "A/B test hero copy using the autoresearch harness and keep winner."

## Concrete Recipes

### 1. Figma-driven landing (zero guesswork)
```bash
# one-time
vxd figma auth   # opens browser, stores token

vxd req "Build marketing landing page exactly matching the provided Figma design at https://www.figma.com/design/XXXX/Vortex-Marketing?node-id=1-42 . Responsive, accessible, fast. Add subtle animations matching design."
```

### 2. Full product launch site + docs
Use one requirement that spans:
- Public marketing site
- /docs section generated from your code comments or separate MDX
- Lead magnet (download form that writes to a simple KV or email)

VXD will plan the dependency DAG correctly.

### 3. Marketing tooling as software
```bash
vxd req "Build a CLI + web tool 'adforge' that takes product name + 3 bullets and emits 10 platform-optimized ad variants (twitter, linkedin, meta) + suggested images alt text. Output static site + generator binary."
```

Now you have a *marketing product* shipped by the same system you use for your core app.

### 4. Autoresearch for on-brand evolution
Configure in vxd.yaml:
```yaml
autoresearch:
  enabled: true
  metric:
    command: "npm run marketing-score -- --site ./dist"
  editable_paths: ["app/marketing", "content/copy"]
  gate: "pr"
```

Then:
```bash
vxd autoresearch start ./   # or specific repo
vxd autoresearch evolve .   # manually evolve program.md for your voice
```

The evolver (now fully wired) learns what copy wins on your metric.

### 5. Weekly self-improvement on marketing patterns
`vxd improve` already runs research + proposals across the repo. Point it at marketing/ or let it discover better patterns for your vertical.

## Tips for Breeze-Level Speed

- Use `--skip-preflight` only after first success (or in CI).
- Set `review_mode: auto` (default) for full hands-off.
- For copy-heavy: give the reviewer/QA role strong instructions via models or custom prompts (see agent/prompts).
- Pair with `vxd learn /path/to/brand-assets` to bootstrap repo profile for better first-pass plans.
- Dashboard + watch for monitoring without staring at tmux.
- For static marketing: add declarative QA criteria like "dist/index.html contains 'Vortex' and all images have alt".

## Training Next Steps

After this guide:
1. Run the tutorial.md with a small marketing req first.
2. Read autonomous-software-development.md for deep pipeline mastery (applies 100% to marketing assets too).
3. Configure autoresearch + improve to make VXD *learn your taste* over time.
4. Ship your first marketing product today.

VXD turns "I need a site" into "PR merged, deployed" while you focus on the idea.

God willing, this makes marketing products truly as easy as ABC.
