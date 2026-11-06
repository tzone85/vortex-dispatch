# Web Dashboard Quiet Horizon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add approved Quiet Horizon video background to embedded VXD web dashboard while preserving behavior, security baseline, responsiveness, accessibility, and LF endings.

**Architecture:** Add decorative video and gradient markup to existing embedded HTML, style it behind unchanged application DOM, and allow only exact CloudFront media origin in CSP. Pin web assets to LF through `.gitattributes` and tests. No application data flow changes.

**Tech Stack:** Go 1.23+, `embed.FS`, `net/http`, vanilla HTML/CSS/JavaScript, Go `testing`, GitHub CLI.

## Global Constraints

- Use exact MP4 URL from approved design.
- Preserve existing dashboard DOM IDs, WebSocket behavior, actions, authentication, and event projections.
- Use vanilla HTML, CSS, and JavaScript; add no React, Tailwind, Lucide, Motion, font, package, or build dependency.
- Keep video decorative, silent, looping, inline, and hidden for `prefers-reduced-motion`.
- Keep static fallback fully readable when remote media fails.
- Allow media only from `https://d8j0ntlcm91z4.cloudfront.net`.
- Use LF endings for every touched source file.
- Build only to `~/.local/bin/vxd`.
- Stage only files belonging to this feature; preserve unrelated dirty working-tree files.

---

### Task 1: Pin static markup, visual contract, and LF wiring

**Files:**
- Create: `internal/web/static_design_test.go`
- Modify: `.gitattributes`
- Modify: `internal/web/static/index.html`
- Modify: `internal/web/static/styles.css`

**Interfaces:**
- Consumes: package-level `staticFiles embed.FS` from `internal/web/server.go`.
- Produces: embedded `.dashboard-background`, `.dashboard-background__video`, and `.dashboard-background__horizon` elements; LF-enforced static sources.

- [ ] **Step 1: Write failing embedded-asset tests**

Create `internal/web/static_design_test.go`:

```go
package web

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

const quietHorizonVideoURL = "https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260428_193507_4286c423-2fd9-4efd-92bd-91a939453fc1.mp4"

func readStaticAsset(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(staticFiles, "static/"+name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	return b
}

func TestDashboardQuietHorizonWiring(t *testing.T) {
	html := string(readStaticAsset(t, "index.html"))
	for _, want := range []string{
		`class="dashboard-background"`,
		`aria-hidden="true"`,
		`class="dashboard-background__video"`,
		"autoplay muted loop playsinline",
		`preload="metadata"`,
		quietHorizonVideoURL,
		`class="dashboard-background__horizon"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing Quiet Horizon wiring %q", want)
		}
	}

	css := string(readStaticAsset(t, "styles.css"))
	for _, want := range []string{
		".dashboard-background {",
		".dashboard-background__video {",
		".dashboard-background__horizon {",
		"@media (prefers-reduced-motion: reduce)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("styles.css missing Quiet Horizon rule %q", want)
		}
	}
}

func TestDashboardStaticSourcesUseLF(t *testing.T) {
	for _, name := range []string{"index.html", "styles.css"} {
		if content := readStaticAsset(t, name); bytes.Contains(content, []byte{'\r'}) {
			t.Errorf("static/%s contains CRLF or stray carriage returns", name)
		}
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/web -run 'TestDashboardQuietHorizonWiring|TestDashboardStaticSourcesUseLF' -count=1
```

Expected: FAIL because Quiet Horizon markup/rules are absent and current `styles.css` working copy contains carriage returns.

- [ ] **Step 3: Force LF checkout policy for web assets**

Add to `.gitattributes` immediately after JSON/Markdown/text rules:

```gitattributes
*.html      text eol=lf
*.css       text eol=lf
*.js        text eol=lf
```

- [ ] **Step 4: Add decorative background markup**

Insert immediately after `<body>` in `internal/web/static/index.html`:

```html
  <div class="dashboard-background" aria-hidden="true">
    <video class="dashboard-background__video" autoplay muted loop playsinline preload="metadata">
      <source src="https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260428_193507_4286c423-2fd9-4efd-92bd-91a939453fc1.mp4" type="video/mp4">
    </video>
    <div class="dashboard-background__horizon"></div>
  </div>
```

- [ ] **Step 5: Implement Quiet Horizon CSS**

Add tokens before reset and replace base/header/section surfaces with these exact rules while retaining existing typography, table, status, control, toast, and dialog behavior:

```css
:root {
  --vxd-bg: #0a1320;
  --vxd-surface: rgba(7, 16, 29, 0.78);
  --vxd-surface-strong: rgba(7, 16, 29, 0.9);
  --vxd-border: rgba(184, 228, 255, 0.14);
  --vxd-cyan: #5ce6e2;
  --vxd-shadow: 0 18px 50px rgba(0, 0, 0, 0.25);
}

body {
  position: relative;
  isolation: isolate;
  min-height: 100vh;
  background: var(--vxd-bg);
  color: #FFFFFF;
  font-family: monospace;
  margin: 0;
  padding: 16px;
  font-size: 13px;
}

.dashboard-background {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
  background:
    radial-gradient(circle at 72% 4%, rgba(55, 113, 140, 0.28), transparent 35%),
    var(--vxd-bg);
}

.dashboard-background__video {
  position: absolute;
  inset: 0 0 auto;
  width: 100%;
  height: min(58vh, 48rem);
  object-fit: cover;
  object-position: center;
  opacity: 0.86;
}

.dashboard-background__horizon {
  position: absolute;
  inset: 0;
  background: linear-gradient(
    180deg,
    rgba(3, 9, 18, 0.2) 0%,
    rgba(6, 15, 27, 0.52) 28%,
    var(--vxd-bg) 52%,
    var(--vxd-bg) 100%
  );
}

body > :not(.dashboard-background) {
  position: relative;
  z-index: 1;
}

#header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border: 1px solid var(--vxd-border);
  border-bottom-color: rgba(92, 230, 226, 0.5);
  border-radius: 12px;
  padding: 10px 12px;
  margin-bottom: 12px;
  background: rgba(6, 14, 26, 0.58);
  box-shadow: var(--vxd-shadow);
  backdrop-filter: blur(16px) saturate(125%);
}

section {
  margin-bottom: 12px;
  border: 1px solid var(--vxd-border);
  border-left: 2px solid var(--vxd-cyan);
  border-radius: 12px;
  padding: 12px;
  background: var(--vxd-surface);
  box-shadow: var(--vxd-shadow);
  backdrop-filter: blur(14px) saturate(120%);
}

@media (max-width: 720px) {
  body {
    padding: 10px;
  }

  .dashboard-background__video {
    height: 42vh;
    object-position: 65% center;
  }

  .dashboard-background__horizon {
    background: linear-gradient(
      180deg,
      rgba(3, 9, 18, 0.24) 0%,
      rgba(6, 15, 27, 0.68) 20%,
      var(--vxd-bg) 40%,
      var(--vxd-bg) 100%
    );
  }
}

@media (prefers-reduced-motion: reduce) {
  .dashboard-background__video {
    display: none;
  }
}
```

Normalize only touched static files to LF after patching:

```bash
perl -pi -e 's/\r$//' internal/web/static/index.html internal/web/static/styles.css
```

- [ ] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
go test ./internal/web -run 'TestDashboardQuietHorizonWiring|TestDashboardStaticSourcesUseLF' -count=1
git check-attr eol -- internal/web/static/index.html internal/web/static/styles.css internal/web/static/app.js
```

Expected: tests PASS; every asset reports `eol: lf`.

- [ ] **Step 7: Commit Task 1**

```bash
git add .gitattributes internal/web/static/index.html internal/web/static/styles.css internal/web/static_design_test.go
git diff --cached --check
git commit -m "feat: add quiet horizon dashboard background"
```

---

### Task 2: Preserve CSP security and document external media

**Files:**
- Modify: `internal/web/auth_test.go`
- Modify: `internal/web/auth.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `authenticator.wrap(http.Handler)` response-header path.
- Produces: exact CSP allowing dashboard video from one CloudFront host; documented remote-media behavior.

- [ ] **Step 1: Strengthen CSP test and verify RED**

Replace final CSP presence assertion in `TestAuthHeaders_SetsSecurityBaseline` with:

```go
	wantCSP := "default-src 'self'; img-src 'self' data:; media-src 'self' https://d8j0ntlcm91z4.cloudfront.net; connect-src 'self' ws: wss:; frame-ancestors 'none'"
	if got := resp.Header.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
```

Run:

```bash
go test ./internal/web -run TestAuthHeaders_SetsSecurityBaseline -count=1
```

Expected: FAIL showing existing CSP lacks exact `media-src` directive.

- [ ] **Step 2: Add narrow media policy and verify GREEN**

Change CSP assignment in `internal/web/auth.go` to:

```go
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' https://d8j0ntlcm91z4.cloudfront.net; connect-src 'self' ws: wss:; frame-ancestors 'none'")
```

Run:

```bash
go test ./internal/web -run TestAuthHeaders_SetsSecurityBaseline -count=1
```

Expected: PASS.

- [ ] **Step 3: Document dashboard media behavior**

Add after web-dashboard commands in README monitoring section:

```markdown
Web dashboard uses Quiet Horizon decorative video from
`d8j0ntlcm91z4.cloudfront.net`, fading into static dark data surface. If remote
media is unavailable—or browser requests reduced motion—dashboard remains fully
usable with static gradient fallback. No dashboard state or controls depend on
video loading.
```

- [ ] **Step 4: Run package regression tests**

Run:

```bash
go test ./internal/web -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/web/auth.go internal/web/auth_test.go README.md
git diff --cached --check
git commit -m "docs: describe dashboard media fallback"
```

---

### Task 3: Verify built dashboard, full suite, and publish

**Files:**
- Verify only; no planned source changes.

**Interfaces:**
- Consumes: committed Tasks 1–2.
- Produces: locally verified binary, review-ready PR, merged branch after required checks.

- [ ] **Step 1: Verify static files and focused package**

```bash
git ls-files --eol internal/web/static/index.html internal/web/static/styles.css internal/web/static/app.js
git diff --check origin/main...HEAD
go test ./internal/web -count=1
```

Expected: indexed/worktree line endings are LF; no whitespace errors; package PASS.

- [ ] **Step 2: Build mandated binary and verify path**

```bash
go build -o ~/.local/bin/vxd ./cmd/vxd
which vxd
```

Expected: build exits 0; `which vxd` prints `~/.local/bin/vxd` expanded to absolute home path.

- [ ] **Step 3: Start local dashboard and inspect behavior**

Use temporary state directory/config when available, start `~/.local/bin/vxd dashboard --web --port 8788 --no-open`, then inspect authenticated page in browser at desktop width and approximately 390px width. Verify exact video loads, fade becomes opaque before dense tables, text/actions remain legible, and blocking remote media leaves static fallback. Stop process after inspection.

Expected: dashboard renders Quiet Horizon without console/CSP media errors; layout remains usable at both widths.

- [ ] **Step 4: Run supported full suite**

```bash
go test $(go list ./... | grep -v improve) -count=1
```

Expected: PASS.

- [ ] **Step 5: Review feature-only diff and working-tree isolation**

```bash
git status --short --branch
git diff --stat origin/main...HEAD
git diff origin/main...HEAD -- .gitattributes README.md docs/superpowers internal/web
git diff --check origin/main...HEAD
```

Expected: branch diff contains only spec, plan, dashboard background, CSP, tests, README, and LF policy. Pre-existing unrelated dirty files remain unstaged and absent from commits.

- [ ] **Step 6: Push, open ready PR, monitor checks, and merge**

Create `/tmp/vxd-quiet-horizon-pr.md` with `apply_patch` and this content:

```markdown
## Summary

- add approved Quiet Horizon video background to embedded VXD web dashboard
- preserve readable static and reduced-motion fallbacks
- allow media only from exact CloudFront host and enforce LF for web assets

## Validation

- `go test ./internal/web -count=1`
- `go test $(go list ./... | grep -v improve) -count=1`
- `go build -o ~/.local/bin/vxd ./cmd/vxd`
- local desktop/mobile dashboard inspection
- `git diff --check origin/main...HEAD`
```

```bash
git push -u origin feat/dashboard-web-background
gh pr create --base main --head feat/dashboard-web-background --title "feat: add quiet horizon web dashboard background" --body-file /tmp/vxd-quiet-horizon-pr.md
PR_NUMBER=$(gh pr view --json number --jq .number)
gh pr checks "$PR_NUMBER" --watch
gh pr merge "$PR_NUMBER" --squash --delete-branch
```

PR body must summarize visual treatment, exact-host CSP change, static/reduced-motion fallback, LF enforcement, and validation commands. Merge only after required checks pass.
