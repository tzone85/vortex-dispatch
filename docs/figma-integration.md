# Figma Design Integration

Reference a figma.com design URL in a requirement and vxd builds the UI to match the referenced frames instead of inventing a design. This guide covers the one-time auth session, how a design flows through the pipeline, and what to do when a pull fails.

## The one interactive step

Every other vxd run is fire-and-forget. A Figma-referencing run is different **once**: Figma's API needs an operator credential, and creating one is a browser action vxd cannot do for you.

```bash
vxd figma auth
```

The command prints the Figma settings URL (it never opens a browser for you), waits for you to paste a personal access token, validates it against Figma's `/v1/me` endpoint, and stores it at `<state_dir>/figma.token` with owner-only permissions. A mistyped token fails right there in the session — never mid-pipeline hours later.

To create the token: figma.com → Settings → Security → Personal access tokens → generate with **File content: Read** scope. Alternatively, export `FIGMA_TOKEN` in the environment; the env var takes precedence over the stored file.

After this single session, Figma runs are as autonomous as everything else. Check at any time with:

```bash
vxd figma status
```

## Using a design in a requirement

Put the Figma URL anywhere in the requirement text:

```bash
vxd req "Build the marketing site per https://www.figma.com/design/AbC123/My-App?node-id=12-345"
```

All figma.com URL forms work — `design`, `file`, `proto`, and `board` links, with or without a `node-id`. A `node-id` scopes the pull to that frame; without one, vxd pulls from the document root. Multiple URLs in one requirement are all pulled.

If no credential is configured, `vxd req` stops immediately — before any planning tokens are spent — and prints the `vxd figma auth` guidance above.

## What flows through the pipeline

1. **Pull.** `figma.BuildDesignContext` fetches the referenced nodes and 2x PNG renders into `.vxd-design/` at the repo root: a `DESIGN_CONTEXT.md` inventory (frame names, dimensions, text styles, solid fills as hex values) plus the rendered images. The directory is gitignored and stripped from story branches — it is working material, not a deliverable.
2. **Plan.** The planner receives the inventory inside a `<design-reference>` data block and is instructed to derive the project's design-token foundation (palette, typography) *from* the design rather than inventing one.
3. **Implement.** Frontend stories (detected by `detectFrontend`) get the design context in their goal prompt — it overrides the generic `FrontendDesignBrief` guidance — and `copyDesignDir` places the PNGs in each frontend worktree so the coding agent can open and study them before writing UI code.

## Safety properties

- Figma layer names are third-party data. `loadDesignContext` scans the pulled markdown for prompt-injection patterns and drops the whole context (with a loud log) on a hit; angle brackets are neutralised so a layer literally named `</design-reference>` cannot break the prompt framing. The context is capped at 16 KB.
- Render downloads only follow Figma CDN hosts — a tampered API response cannot redirect a download to an internal address.
- The token never appears in logs, error messages, or command output; status output shows the account handle and email only.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `no Figma credential found` at `vxd req` | Run `vxd figma auth` once, or export `FIGMA_TOKEN`. |
| `token validation failed (not stored)` during auth | The pasted token is wrong or lacks the File content: Read scope — generate a fresh one. |
| `all N Figma design reference(s) failed to fetch` | The token's account cannot access the file(s), or the links are stale. Open each URL in the browser under the same account. |
| `figma token file ... unreadable` | Permissions problem on `<state_dir>/figma.token` — `chmod 600` it or re-run `vxd figma auth`. |
| Design context silently absent from prompts | Check the run log for a `prompt-injection pattern` drop — rename the offending Figma layer and re-run. |
