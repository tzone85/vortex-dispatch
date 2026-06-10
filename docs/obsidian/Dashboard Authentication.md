---
tags: [security, web, operations]
---

# Dashboard Authentication

The web dashboard (`vxd dashboard --web`) exposes a WebSocket command channel
that can pause/resume requirements, kill agents, and edit stories. It is
protected by a **per-session token**, because binding to `localhost` alone is
not enough — every process on the host shares localhost.

## How it works (`internal/web/`)
1. **Token generation** — `NewServer` creates a `crypto/rand` token
   (`auth.go: newAuthToken`).
2. **Handshake** — the server opens the browser at `http://localhost:<port>/?token=…`.
   `serveIndex` echoes the token into the page (`window.__VXD_TOKEN__`) and a
   `Strict`-SameSite cookie **only when the request already presents it**, so an
   unrelated local process cannot harvest the token by fetching `/`.
3. **Gate** — `HandleWebSocket` rejects any `/ws` connection whose token doesn't
   match (constant-time `subtle.ConstantTimeCompare`) before upgrading.
4. **CSWSH** — `OriginPatterns` additionally blocks cross-origin browser pages.
   The token covers no-Origin (non-browser) local clients.

## Why both layers
- The **Origin check** stops a malicious web page (browsers always send Origin).
- The **token** stops a local process (which sends no Origin and would otherwise
  slip past the Origin check).

## Operator note
If you navigate manually, use the tokenized URL printed in the terminal. A plain
`http://localhost:<port>/` load will connect read-only-less (no token → `/ws`
returns 401) and the dashboard shows an unauthorized state.

## Related
- [[Security Model]] · [[Security Audit 2026-06]] (finding C1)
