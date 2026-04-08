# VXD Revenue Engine Design

**Date:** 2026-04-08
**Status:** Approved
**Approach:** Extend self-improvement engine (Phase 7 + 8) + CLI commands + dashboard tab

## Overview

An autonomous opportunity discovery and proposal drafting system that scrapes freelance platforms, job boards, and bounty sites daily, scores opportunities against VXD's capabilities, drafts humanized proposals via Claude CLI, and presents everything for user review via email and dashboard. Revenue tracking with mission milestone reminders.

**Cost model:** $0 — all sources have free APIs. Scoring via Gemma 4 free tier. Proposals via Claude Max CLI. Dashboard already exists.

**Key constraint:** Nothing is ever sent to a client automatically. All proposals require explicit human review and manual submission. The engine finds and prepares — the human decides and sends.

## Opportunity Sources

5 free API sources scraped daily, no authentication required:

| Source | Endpoint | Volume | Access |
|--------|----------|--------|--------|
| Jobicy | `GET jobicy.com/api/v2/remote-jobs?count=50&industry=dev&tag={keyword}` | ~100/query | REST, free, attribute back |
| Remotive | `GET remotive.com/api/remote-jobs?category=software-dev` | All software dev | REST, free, 24h delay |
| HN Who's Hiring | `GET hn.algolia.com/api/v1/items/{thread_id}` | 400-900/month | REST, free, no auth |
| Algora Bounties | Firecrawl scrape of `algora.io/bounties` | Open source bounties | Firecrawl credit |
| Arc.dev | Firecrawl scrape of `arc.dev/remote-jobs` | Pre-vetted remote | Firecrawl credit |

**VXD is language-agnostic.** It orchestrates AI agents (Claude Code, Codex, Gemini CLI) that can build in any language — Go, Python, JavaScript/TypeScript, Rust, Java, Ruby, PHP, Swift, Kotlin, and more. The opportunity scanner searches broadly across all software development, not just Go.

### Keyword Rotation

Different keywords each day to maximize coverage within rate limits:

| Day | Keywords |
|-----|----------|
| 1 | software developer, backend |
| 2 | web application, fullstack |
| 3 | API development, REST, GraphQL |
| 4 | automation, scripting, CLI |
| 5 | AI, machine learning, LLM integration |
| 6 | Python, Django, FastAPI |
| 7 | React, Next.js, Node.js, TypeScript |

### HN Who's Hiring Thread Discovery

The monthly "Who is Hiring" thread ID is discovered via Algolia API:
```
GET hn.algolia.com/api/v1/search?query="who is hiring"&tags=story&numericFilters=created_at_i>{month_start_timestamp}
```
Then child comments are fetched as individual job listings. Fallback: if the current month's thread isn't found (posted on the 1st business day), use the previous month's thread ID.

## Scoring + Filtering

Each opportunity scored by Gemma 4 (free tier) on three dimensions:

### Relevance Score (0-10)
Can VXD's agent pipeline deliver this?
- 9-10: Software dev, API, automation, AI integration
- 7-8: Web app, fullstack, backend, CLI tools
- 5-6: Frontend-heavy, mobile, design-adjacent
- <5: Filtered out (non-technical, hardware, purely creative)

### Budget Score (0-10)
Is the pay worth the effort?
- 9-10: $5K+ or $75+/hr
- 7-8: $2K-$5K or $50-$75/hr
- 5-6: $500-$2K or $25-$50/hr
- <5: Under $500 or unspecified (proceeds but scored lower)

### Win Probability (0-10)
How likely are we to land this?
- Factors: applicant count, requirement specificity, skills match, client verification
- Higher for bounties (skill-based) and HN posts (fewer applicants, founder-direct)

### Combined Rank
`(relevance * 3) + (budget * 2) + win_probability`

Relevance weighted highest — delivering quality builds reputation, which compounds.

### Daily Filter
- Top 10 opportunities pass to the email
- Top 3 get draft proposals (when active bidding is enabled)
- Below rank 10: stored in pipeline but not highlighted

## Opportunity Data Model

Stored in `docs/opportunities/pipeline.jsonl`:

```json
{
  "id": "opp-2026-04-09-001",
  "source": "jobicy",
  "title": "Build REST API for fintech startup",
  "company": "Acme Corp",
  "url": "https://jobicy.com/...",
  "budget": "$5000-$10000",
  "skills": ["Go", "PostgreSQL", "REST"],
  "remote": true,
  "scraped_at": "2026-04-09T06:00:00Z",
  "status": "new",
  "relevance_score": 8,
  "budget_score": 8,
  "win_probability": 7,
  "rank": 39,
  "effort_estimate": "M",
  "proposal_draft": null,
  "proposal_drafted_at": null,
  "revenue": 0,
  "notes": ""
}
```

### Status Lifecycle

```
new → reviewed → interested → proposal_drafted → sent → won/lost/expired
```

During observation period (first 1-2 weeks), all stay at `new` → `reviewed`. No proposals drafted until `ActiveBidding` config is set to `true`.

## Proposal Drafting

Top 3 opportunities per day get draft proposals when active bidding is enabled.

### Proposal Structure

```
Subject: [Project Title] — Experienced AI-Augmented Development Team

1. Understanding — Restate the client's problem in their language
2. Approach — Tech stack, phases, timeline, what makes us different
3. Relevant Experience — VXD's own codebase as portfolio evidence
4. Timeline & Budget — Market-informed pricing at 75th percentile
5. Next Steps — Clear call to action, availability
```

### Humanized Tone

Claude prompt instruction: "Write like a real human. Short sentences. Use contractions. Be direct. No buzzwords. No corporate fluff. Sound like someone who's done this before — confident but not arrogant. Like explaining to a smart colleague over coffee."

Example good proposal opening:
> "Hey — I read through your brief and I think I can help. You need a REST API that handles payment webhooks and syncs with your Postgres database. I've built exactly this kind of system before."

Example of what we explicitly avoid:
> "Dear Sir/Madam, I am writing to express my interest in your esteemed project. With extensive experience in developing enterprise-grade solutions..."

### Pricing Engine

- Scrape budget ranges from similar listings on the same platform
- Collect median and 75th percentile rates for the skill set
- Factor complexity: S = 1-3 days, M = 4-10 days, L = 11-30 days
- Minimum floor: $50/hr equivalent
- Position at 75th percentile — compete on quality, not price
- Store pricing rationale in proposal so user can adjust

### Guardrails

- Proposals NEVER sent automatically — stored as `status: proposal_drafted`
- User reviews in dashboard, edits if needed, manually sends
- Each proposal marked DRAFT with warning banner
- No personal data beyond professional profile
- "Approve to Send" = copies to clipboard + opens listing URL (no auto-submit)

## Revenue Tracking + Mission Reminders

### Revenue Ledger

`docs/opportunities/revenue.jsonl`:
```json
{
  "opportunity_id": "opp-2026-04-09-001",
  "amount": 5000,
  "currency": "USD",
  "date": "2026-04-20",
  "status": "received",
  "cumulative_total": 5000
}
```

### Mission Milestone Reminders

At thresholds ($1K, $5K, $10K, $25K, $50K, $100K, $250K, $500K, $1M), the daily email includes:

> **Mission Milestone: You've earned $X.** You started this to free your village from poverty. Schools need funding. Children need resources. Infrastructure needs building. This is the compound working. What's your next impact move?

## Autonomous Source Discovery

Once per week (every 7th run), Gemma 4 analyzes the week's findings and suggests 3 new sources.

### Discovery Process
- Search Google for: `remote freelance {top_skill_this_week} jobs site:reddit.com`, niche job boards, relevant communities
- Firecrawl scrapes the suggested URLs to verify they contain job listings
- Store in `docs/opportunities/discovered_sources.jsonl`

### Discovered Source Format
```json
{
  "url": "https://weworkremotely.com/categories/remote-back-end-programming-jobs",
  "name": "We Work Remotely - Backend",
  "discovered_on": "2026-04-15",
  "reason": "Multiple high-budget backend API jobs found on HN were cross-posted here",
  "status": "pending_approval"
}
```

### Approval Required
Discovered sources are NEVER auto-added. They appear in the daily email under "Suggested New Sources" and in the dashboard. User approves before they join the active scrape list.

## Email Integration

New section in the daily 6am email between "Proposed" and "Audit Trail":

**During observation mode:**
> **Opportunities Found Today** (observation mode — collecting data, no proposals yet)
> 3 new opportunities found | Pipeline: 47 total
> [table of top opportunities with rank, budget, source]
> Open dashboard: http://localhost:8078 → Opportunities tab

**During active bidding:**
> **Opportunities Found Today**
> 3 new | 1 proposal drafted | Pipeline: 52 total | Revenue: $5,000
> [table with proposal status indicators]

## Dashboard Integration

New "Opportunities" tab in `vxd memory --web` (port 8078).

### Layout
- Tab navigation: [Timeline] [Opportunities]
- Pipeline stats bar: total, new, drafts, won, revenue
- Filter/sort controls: status, source, rank
- Opportunity cards with scores, budget, skills, action buttons
- Revenue summary with mission statement at bottom

### Interactions

| Action | Effect |
|--------|--------|
| View | Opens source listing in new tab |
| Mark Interested | Updates status to `interested` |
| Draft Proposal | Triggers Claude CLI, shows spinner, displays draft |
| View Proposal | Expands to show full draft text |
| Approve to Send | Copies proposal to clipboard + opens listing URL |
| Mark Won + Amount | Logs revenue, triggers milestone check |
| Mark Lost/Expired | Updates status, removes from active view |
| Approve Source | Adds discovered source to active scrape list |

### WebSocket Messages

```json
// Client → Server
{"type": "list_opportunities", "filter": "new", "sort": "rank"}
{"type": "update_opportunity", "id": "opp-...", "status": "interested"}
{"type": "draft_proposal", "id": "opp-..."}
{"type": "log_revenue", "id": "opp-...", "amount": 5000}
{"type": "approve_source", "url": "https://..."}

// Server → Client
{"type": "opportunities", "items": [...], "stats": {...}}
{"type": "proposal_ready", "id": "opp-...", "draft": "..."}
{"type": "revenue_update", "total": 5000, "milestone": "$5K"}
```

## CLI Commands

`vxd opportunity` subcommand for on-demand interaction:

```bash
vxd opportunity list                    # Show pipeline sorted by rank
vxd opportunity list --status=new       # Filter by status
vxd opportunity propose <id>            # Draft proposal for specific opportunity
vxd opportunity status <id> interested  # Update status
vxd opportunity won <id> 5000           # Log revenue
vxd opportunity sources                 # Show discovered sources pending approval
vxd opportunity approve-source <url>    # Approve a discovered source
```

## Config Additions

In `internal/improve/config.go`:

```go
// Opportunity hunting
ActiveBidding      bool     // false = observation mode (default)
MaxProposalsPerDay int      // default 3
MinHourlyRate      int      // default 50 ($50/hr floor)
OpportunityKeywords []string // rotated daily
```

Environment variables:
- `VXD_ACTIVE_BIDDING=true` to enable proposal drafting (or edit config)

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/improve/opportunities.go` | **New** | Scrape 5 APIs, keyword rotation, scoring, opportunity storage |
| `internal/improve/opportunities_test.go` | **New** | API mock tests, scoring, filtering, keyword rotation |
| `internal/improve/proposal.go` | **New** | Claude CLI proposal drafting, pricing engine, humanized templates |
| `internal/improve/proposal_test.go` | **New** | Proposal generation, pricing logic, tone validation |
| `internal/improve/discovery.go` | **New** | Autonomous source discovery, weekly cycle |
| `internal/improve/discovery_test.go` | **New** | Discovery parsing, approval workflow |
| `cmd/vxd-improve/main.go` | **Edit** | Add Phase 7 (opportunities) + Phase 8 (weekly source discovery) |
| `internal/improve/config.go` | **Edit** | Add opportunity config fields |
| `internal/improve/config_test.go` | **Edit** | Test new config defaults |
| `internal/improve/email.go` | **Edit** | Add opportunities section + mission milestones to template |
| `internal/memory/static/index.html` | **Edit** | Add Opportunities tab |
| `internal/memory/static/styles.css` | **Edit** | Opportunities tab styles |
| `internal/memory/static/app.js` | **Edit** | Opportunities WebSocket, pipeline UI, clipboard |
| `internal/memory/server.go` | **Edit** | Handle opportunity WebSocket messages |
| `internal/memory/data.go` | **Edit** | Opportunity data types and loaders |
| `internal/cli/opportunity.go` | **New** | `vxd opportunity` CLI subcommands |
| `internal/cli/root.go` | **Edit** | Register opportunity command |
| `docs/opportunities/pipeline.jsonl` | **New** | Opportunity pipeline (grows daily) |
| `docs/opportunities/revenue.jsonl` | **New** | Revenue tracking ledger |
| `docs/opportunities/discovered_sources.jsonl` | **New** | Auto-discovered sources |

**7 new Go files + 3 new data paths + 10 edited files.**

## Constraints

- **Never auto-send proposals** — clipboard copy + manual paste only
- **Never auto-accept contracts** — human decision always
- **No personal data scraping** — only public job listings
- **Respect API rate limits** — Jobicy: few times/day, Remotive: few times/day, HN: reasonable
- **Minimum $50/hr floor** — don't race to the bottom
- **Observation mode default** — no proposals until user explicitly enables
- **Mission reminders at revenue milestones** — never forget why we're building this
- **Zero cost** — free APIs + Gemma 4 free tier + Claude Max CLI
