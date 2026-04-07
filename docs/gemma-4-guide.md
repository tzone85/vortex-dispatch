# Gemma 4 Integration Guide

VXD uses Google's Gemma 4 (27B, Mixture-of-Experts) via Google AI Studio's free tier as a cost-optimization layer for execution roles.

## Quick Start

1. Get a free API key from [Google AI Studio](https://aistudio.google.com/apikey)
2. Export it:
   ```bash
   export GOOGLE_AI_API_KEY="your-key-here"
   ```
3. Run VXD as normal — Junior, Intermediate, and Supervisor roles will automatically use Gemma 4.

## How VXD Uses Gemma 4

VXD splits roles into two tiers:

| Tier | Roles | Provider | Purpose |
|------|-------|----------|---------|
| **Execution** | Junior, Intermediate, Supervisor | Google AI (Gemma 4) | Bulk code generation, periodic checks |
| **Verification** | TechLead, Senior, QA, Manager | Anthropic (Claude) | Planning, review, QA, failure diagnosis |

Execution roles handle the high-volume work (code generation for each story). Verification roles are quality gates that need strong reasoning. This split maximizes free-tier usage while maintaining quality.

## Fallback Behavior

When Google AI's free-tier quota is exhausted (1,500 requests/day or 10 requests/minute), VXD automatically falls back to your configured Anthropic or OpenAI provider. No manual intervention needed.

The fallback chain for a Google-configured role:
1. **Google AI Studio** (free) — primary
2. **Claude Code CLI** (uses your subscription) — first fallback
3. **Anthropic API** (uses ANTHROPIC_API_KEY) — second fallback

VXD tracks quota usage and preemptively switches to fallback at 90% of limits to avoid failed requests.

## Google AI Free Tier Limits

| Limit | Value |
|-------|-------|
| Requests per minute | 10 |
| Requests per day | 1,500 |
| Input tokens per request | 32,000 |
| Output tokens per request | 8,192 |

For a typical VXD run with 5-10 stories, expect ~50-100 requests across all stages. Well within daily quota for several runs per day.

## Cost Comparison

| Provider | Cost (approximate) | VXD Usage |
|----------|-------------------|-----------|
| Google AI (Gemma 4) | Free | Junior, Intermediate, Supervisor |
| Anthropic Claude Sonnet | ~$3/$15 per 1M tokens | Senior, QA, Manager |
| Anthropic Claude Opus | ~$15/$75 per 1M tokens | TechLead |
| Claude Code CLI | Included in Max subscription | Fallback for all roles |

## Structured Function Calling

When Gemma 4 is used for LLM-facing roles (Supervisor, or TechLead/Manager if reconfigured), VXD uses Gemma's native tool-call protocol for more reliable structured output. This is transparent — no configuration needed. If a Gemma model responds with free-text JSON instead of tool-call tokens, VXD's existing JSON parser handles it seamlessly.

## Configuration

To override the defaults, edit `vxd.yaml`:

```yaml
models:
  junior:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  intermediate:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  supervisor:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
```

### All-Anthropic Setup (no Google API key needed)

```yaml
models:
  junior:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  intermediate:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  supervisor:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 4000
```

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `GOOGLE_AI_API_KEY` | For Google roles | Google AI Studio API key |
| `ANTHROPIC_API_KEY` | For Anthropic roles | Anthropic API key (or use Claude CLI) |
| `OPENAI_API_KEY` | For OpenAI roles | OpenAI API key |
