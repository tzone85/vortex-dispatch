# Gemma 4 + Google AI Integration Design

**Date:** 2026-04-07
**Status:** Approved
**Approach:** Composable Client Wrappers (Approach A)

## Overview

Integrate Google's Gemma 4 (27B, MoE architecture) into VXD as a cost-optimization layer for execution roles. Google AI Studio's free tier serves Junior, Intermediate, and Supervisor roles, while Anthropic's Claude stays on verification and planning roles (TechLead, Senior, QA, Manager). A `FallbackClient` transparently escalates to paid Anthropic/OpenAI providers when Google's free-tier quota is exhausted.

## Role-to-Model Strategy

| Role | Provider | Model | Tier | Rationale |
|------|----------|-------|------|-----------|
| TechLead | Anthropic | claude-opus-4-20250514 | Verification | Deep reasoning for requirement decomposition |
| Senior | Anthropic | claude-sonnet-4-20250514 | Verification | Complex code, escalations |
| Intermediate | Google AI | gemma-4-27b-it | Execution | Cost-efficient for medium stories (3-5 pts) |
| Junior | Google AI | gemma-4-27b-it | Execution | Cost-efficient for simple stories (1-3 pts) |
| QA | Anthropic | claude-sonnet-4-20250514 | Verification | Strong verification gate — catches plan-vs-execution drift |
| Supervisor | Google AI | gemma-4-27b-it | Execution | Periodic drift checks (lightweight) |
| Manager | Anthropic | claude-sonnet-4-20250514 | Verification | Failure diagnosis requires strong reasoning |

**Design principle:** Execution roles (bulk token spend, code generation) use free Gemma 4. Verification roles (quality gates, planning, diagnosis) use paid Claude for reasoning depth. QA specifically stays on Claude because it bridges the gap between planned intent and actual execution — it needs comparable reasoning capability to the planner.

## Architecture

### Composable Client Wrapper Chain

VXD's existing `Client` interface and decorator pattern (`RetryClient`) are extended with three new wrappers:

```
RetryClient
  └─ FallbackClient
       ├─ primary:  ToolCallAdapter → GoogleAIClient
       └─ fallback: ToolCallAdapter → AnthropicClient (or ClaudeCLIClient)
```

Each wrapper has a single responsibility:
- **RetryClient** — transient retry with exponential backoff (existing)
- **FallbackClient** — provider-level switching on quota exhaustion
- **ToolCallAdapter** — Gemma tool-call format injection/parsing
- **GoogleAIClient** — HTTP transport to Google AI Studio

### 1. GoogleAIClient (`internal/llm/google.go`)

New `Client` implementation using direct HTTP (no SDK), matching the pattern of `AnthropicClient` and `OpenAIClient`.

**Constructor:**
- `NewGoogleAIClient(apiKey string) *GoogleAIClient`
- `WithBaseURL(url string) *GoogleAIClient` — for httptest servers

**API endpoint:**
```
POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent?key={apiKey}
```

**Request mapping (VXD → Google):**

| VXD Field | Google AI Field |
|-----------|----------------|
| `System` | `system_instruction.parts[].text` |
| `Messages[].Role == "user"` | `contents[].role = "user"` |
| `Messages[].Role == "assistant"` | `contents[].role = "model"` |
| `Messages[].Content` | `contents[].parts[].text` |
| `MaxTokens` | `generationConfig.maxOutputTokens` |
| `Temperature` | `generationConfig.temperature` |

**Response mapping (Google → VXD):**

| Google AI Field | VXD Field |
|----------------|-----------|
| `candidates[0].content.parts[0].text` | `CompletionResponse.Content` |
| `modelVersion` | `CompletionResponse.Model` |
| `candidates[0].finishReason` | `CompletionResponse.StopReason` |
| `usageMetadata.promptTokenCount` | `Usage.InputTokens` |
| `usageMetadata.candidatesTokenCount` | `Usage.OutputTokens` |

**Error mapping:**

| Google HTTP Status | APIError | Retryable |
|-------------------|----------|-----------|
| 429 | Rate limited | Yes |
| 400 + `RESOURCE_EXHAUSTED` | Quota exhausted (mapped to StatusCode 429 in APIError so existing `IsRateLimited()` catches it) | Yes (triggers fallback) |
| 403 | API key invalid | No (fatal) |
| 5xx | Server error | Yes |

### 2. FallbackClient (`internal/llm/fallback.go`)

Wrapper that tries a primary `Client` and falls back to a secondary on quota/rate-limit errors.

**Structure:**
```go
type FallbackClient struct {
    primary      Client
    fallback     Client
    quotaTracker *QuotaTracker
}
```

**Behavior flow:**
1. Check `quotaTracker.IsExhausted()` — if true, skip primary, use fallback directly.
2. Try `primary.Complete()`.
3. On success → record request in tracker, return response.
4. On 429 or `RESOURCE_EXHAUSTED` → mark quota exhausted with cooldown, try fallback.
5. On other retryable errors (5xx) → try fallback immediately, don't mark quota.
6. On fatal errors (401, 403) → try fallback, log provider misconfiguration warning.
7. If fallback also fails → return fallback error.

**QuotaTracker** (embedded in `fallback.go`, not a separate file):
- Tracks requests-per-minute (RPM) and requests-per-day (RPD) with atomic counters.
- Google AI free-tier limits for Gemma: 10 RPM, 1500 RPD.
- `IsExhausted() bool` — returns true if either counter is at 90% of limit.
- `RecordRequest()` — increments counters.
- `MarkExhausted(cooldown time.Duration)` — called on 429, suppresses primary for the cooldown period.
- Counter reset: checks `time.Now()` against minute/day boundaries on each call. No background goroutines.
- RPM and RPD limits are configurable via constructor for testability.

**Placement in the wrapper chain:** Between `RetryClient` (outer) and the actual providers (inner). `RetryClient` handles transient retries on whichever provider wins. `FallbackClient` handles provider-level switching.

### 3. ToolCallAdapter (`internal/llm/toolcall.go`)

Middleware that transparently adds structured function calling for Gemma models while passing through unchanged for others.

**Structure:**
```go
type ToolCallAdapter struct {
    inner      Client
    toolSchema *ToolSchema // nil = passthrough mode
}
```

**Behavior:**
1. **Detection:** On `Complete()`, check if `req.Model` contains `"gemma"`. If not → passthrough to `inner.Complete()` unchanged.
2. **Request augmentation (Gemma only):** Append tool definitions to the system prompt:
   ```
   Available tools:
   <tools>
   [{"name": "create_stories", "parameters": {...}}]
   </tools>
   ```
   Original system prompt preserved as prefix. User message unchanged.
3. **Response parsing (Gemma only):** Scan response for `<|tool_call|>` tokens. If found → extract structured JSON, return as `CompletionResponse.Content`. If not found → return content as-is (graceful degradation).

**Graceful degradation:** If Gemma responds with free-text JSON instead of tool-call tokens, the adapter returns raw content. The engine's existing `extractJSON()` handles it. No new failure mode.

**Key invariant:** The adapter produces the same JSON structures that engine code already parses. Zero changes to engine files.

### 4. Tool Schemas (`internal/llm/toolschemas.go`)

Per-role tool definitions that mirror existing engine JSON structures:

| Role | Tool Name | Key Fields |
|------|-----------|------------|
| TechLead | `create_stories` | `stories[]` — id, title, description, acceptance_criteria, complexity, depends_on, owned_files, wave_hint |
| Reviewer | `submit_review` | `passed` (bool), `comments[]` (file, line, severity, comment), `summary` |
| Supervisor | `report_status` | `on_track` (bool), `concerns[]`, `reprioritize[]` |
| Manager | `diagnose_failure` | `diagnosis`, `category`, `action`, action-specific config (retry_config, rewrite_config, split_config) |

**Schema selection:** `ToolSchemaFor(role agent.Role) *ToolSchema` returns the appropriate schema. Returns `nil` for roles that don't make direct LLM calls (Junior, Intermediate, Senior — these use CLI runtimes, not the `Client` interface).

## Config Changes

### `internal/config/loader.go` — DefaultConfig()

Update three roles in `ModelsConfig`:

```go
Intermediate: ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
Junior:       ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
Supervisor:   ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
```

All other roles unchanged.

### `internal/cli/req.go` — buildLLMClient() and buildPlanningClient()

Add `"google"` case:

```go
case "google":
    apiKey := os.Getenv("GOOGLE_AI_API_KEY")
    if apiKey == "" {
        return nil, fmt.Errorf("GOOGLE_AI_API_KEY environment variable is required")
    }
    google := NewToolCallAdapter(NewGoogleAIClient(apiKey), schema)
    var fallback Client
    if _, err := exec.LookPath("claude"); err == nil {
        fallback = NewClaudeCLIClient()
    } else if ak := os.Getenv("ANTHROPIC_API_KEY"); ak != "" {
        fallback = NewRetryClient(NewAnthropicClient(ak), 3)
    }
    primary := NewRetryClient(google, 2)
    if fallback != nil {
        return NewFallbackClient(primary, fallback), nil
    }
    return primary, nil
```

**Signature change:** `buildLLMClient(provider string, schema *llm.ToolSchema, godmode ...bool)`. Existing callers pass `nil` for schema (passthrough mode). `buildPlanningClient` similarly gains a schema parameter — callers pass `llm.ToolSchemaFor(agent.RoleTechLead)`.

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/llm/google.go` | **New** | GoogleAIClient — direct HTTP to Google AI Studio |
| `internal/llm/fallback.go` | **New** | FallbackClient + QuotaTracker — provider-level switching |
| `internal/llm/toolcall.go` | **New** | ToolCallAdapter — Gemma tool-call injection/parsing |
| `internal/llm/toolschemas.go` | **New** | Per-role tool schema definitions |
| `internal/config/loader.go` | **Edit** | DefaultConfig() — 3 roles switch to Google/Gemma 4 |
| `internal/cli/req.go` | **Edit** | Add "google" provider to buildLLMClient() and buildPlanningClient() |
| `docs/gemma-4-guide.md` | **New** | Integration guide: setup, cost comparison, fallback behavior |
| `docs/model-selection.md` | **Edit** | Add Google provider, execution/verification tier concept |
| `docs/getting-started.md` | **Edit** | Add GOOGLE_AI_API_KEY, quick-start note |

## Files NOT Changed (by design)

- `internal/llm/client.go` — `Client` interface unchanged
- `internal/llm/errors.go` — `APIError` already handles all needed error codes
- `internal/config/config.go` — `ModelConfig` type unchanged
- `internal/engine/planner.go` — provider-agnostic, ToolCallAdapter handles format
- `internal/engine/reviewer.go` — same
- `internal/engine/supervisor.go` — same
- `internal/engine/manager.go` — same
- `internal/engine/executor.go` — providerRuntimes already maps "google" to "gemini"

## Constraints

- **Cloud-first, cost-optimized:** Google AI free tier is the cost layer. Anthropic/OpenAI are the reliability backbone.
- **Fallback direction:** Escalates UP to paid providers (reverse of NXD's fallback-to-local).
- **Free usage only:** No paid Google APIs. The free tier is 1500 RPD / 10 RPM for Gemma.
- **Graceful degradation:** All tool-call schemas degrade to free-text JSON parsing for non-Gemma models.
- **Zero engine changes:** ToolCallAdapter produces the same JSON structures engine code already parses.

## Google AI Free Tier Limits (Gemma 4)

| Limit | Value |
|-------|-------|
| Requests per minute | 10 |
| Requests per day | 1,500 |
| Input tokens per request | 32,000 |
| Output tokens per request | 8,192 |

For a typical VXD run with 5-10 stories, expect ~50-100 requests (planning + execution + supervision). Well within daily quota for several runs per day.
