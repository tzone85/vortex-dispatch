# Web Dashboard Quiet Horizon Design

**Feature branch:** `feat/dashboard-web-background`

**Created:** 2026-08-06

**Status:** Approved
**Input:** Improve VXD web dashboard background using exact RIVR video source while preserving existing behavior, tests, wiring, and LF line endings.

## Goal

Give web dashboard premium moving backdrop without reducing operational readability or changing dashboard behavior.

## User Scenario

### View pipeline state in polished web dashboard (P1)

Operator opens `vxd dashboard --web` and sees existing live dashboard over Quiet Horizon treatment: motion across upper field fading into stable dark data surface.

**Why this priority:** Web users need attractive interface without losing fast access to agents, stories, activity, escalations, and controls.

**Independent test:** Start local web dashboard, open it at desktop and mobile widths, and verify video atmosphere, table readability, real-time updates, and controls.

**Acceptance scenarios:**

1. **Given** browser permits motion and remote media loads, **when** operator opens dashboard, **then** exact configured MP4 autoplays silently, loops, and fades into opaque navy before dense data area.
2. **Given** remote media cannot load, **when** operator opens dashboard, **then** static navy gradient provides complete readable background without layout shift.
3. **Given** browser requests reduced motion, **when** operator opens dashboard, **then** video is hidden and static fallback remains.
4. **Given** dashboard receives WebSocket data or operator uses existing actions, **when** UI updates, **then** existing DOM IDs, handlers, tables, dialogs, and status behavior remain unchanged.
5. **Given** repository checkout, **when** static dashboard sources are inspected, **then** touched HTML, CSS, Go, and Markdown files use LF endings only.

## Visual Design

### Quiet Horizon

- Exact video source: `https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260428_193507_4286c423-2fd9-4efd-92bd-91a939453fc1.mp4`.
- Video occupies fixed background and remains most visible in upper approximately 42% of viewport.
- Layered navy gradient transitions motion into opaque `#0a1320` field before dense data.
- Existing dashboard content remains foreground. Header and sections use restrained translucent dark surfaces, blur, and subtle cyan borders for contrast.
- Existing VXD monospace character and semantic status colors remain recognizable.
- Mobile narrows video crop and moves opaque transition upward.
- Reduced-motion preference disables moving layer.

## Architecture

### Static markup

Add decorative background container near start of `body` in `internal/web/static/index.html`. Container is `aria-hidden="true"` and contains `<video autoplay muted loop playsinline>` with exact source URL plus non-interactive gradient layer. Existing dashboard nodes retain IDs and order relative to application JavaScript.

### Styling

Update `internal/web/static/styles.css` with LF-only content. Define reusable color and surface tokens in `:root`, fixed background layers, foreground stacking context, glass surfaces, responsive behavior, static fallback, and reduced-motion behavior. No framework, downloaded font, new JavaScript, or frontend build step is introduced.

### Security policy

Update dashboard Content Security Policy in `internal/web/auth.go` with narrow `media-src 'self' https://d8j0ntlcm91z4.cloudfront.net` directive. Keep every existing directive unchanged. Do not permit broad `https:` media sources.

### Documentation

Update README web-dashboard documentation to disclose remote decorative video, static fallback, reduced-motion behavior, and exact outbound media host.

## Data Flow

No application data flow changes. WebSocket connection, REST endpoints, rendering functions, command handlers, authentication, and event projections remain untouched. Browser fetches decorative video directly from allowed CloudFront host; media success or failure never blocks dashboard content.

## Failure Handling

- Remote timeout, HTTP error, unsupported codec, or CSP rejection leaves CSS fallback visible.
- Video receives no controls and no functional events.
- Reduced-motion users receive static fallback automatically through CSS.
- Narrow viewport changes crop/fade only; dashboard content remains scrollable.

## Testing Strategy

Follow TDD:

1. Add static wiring test that reads embedded `index.html` and asserts decorative structure, exact source, accessibility, and playback attributes.
2. Add CSP test that requires exact CloudFront `media-src` host while preserving existing security directives.
3. Add source-hygiene test that fails on CRLF in touched static HTML/CSS.
4. Run focused `internal/web` tests.
5. Build binary to `~/.local/bin/vxd`, verify resolved binary with `which vxd`, start dashboard locally, and inspect desktop/mobile rendering plus media fallback.
6. Run full repository test command excluding `internal/improve` as required by project instructions.
7. Run `git diff --check` and line-ending inspection before commit.

## Scope Boundaries

### In scope

- Background video and fallback
- Quiet Horizon gradient and restrained glass treatment
- Exact-host CSP allowance
- Responsive and reduced-motion behavior
- Wiring, security, line-ending, local visual, build, and regression verification
- README disclosure

### Out of scope

- RIVR branding or DeFi copy
- React, Tailwind, Lucide, Motion, custom fonts, or frontend toolchain
- Dashboard data model, commands, WebSocket behavior, or control redesign
- New tracking, analytics, cookies, or remote scripts

## Success Criteria

- Exact MP4 visibly supplies upper-page atmosphere when available.
- All dashboard text and controls remain readable across moving bright and dark frames.
- Static fallback renders complete interface with video blocked.
- Reduced-motion preference removes animation.
- Existing web tests and full supported test suite pass.
- Build installs only to `~/.local/bin/vxd` and resolved binary is verified.
- Touched source files contain LF only; `git diff --check` reports no whitespace errors.
- PR contains only design/background-related files, excluding pre-existing unrelated working-tree changes.
