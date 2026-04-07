# Model Selection Guide

VXD supports multiple LLM providers and lets you assign different models to different roles based on your cost, quality, and availability requirements.

## Providers

| Provider | Key | Models | Cost |
|----------|-----|--------|------|
| `anthropic` | `ANTHROPIC_API_KEY` or Claude CLI | Claude Opus/Sonnet/Haiku | Paid (or subscription) |
| `openai` | `OPENAI_API_KEY` | GPT-4o, GPT-4o-mini, o3 | Paid |
| `google` | `GOOGLE_AI_API_KEY` | Gemma 4 27B | Free tier (1500 req/day) |

## Execution vs Verification Tiers

VXD roles fall into two tiers with different quality requirements:

**Execution Tier** — High volume, code generation focus:
- Junior (simple stories, 1-3 complexity)
- Intermediate (medium stories, 3-5 complexity)
- Supervisor (periodic drift checks)

These roles benefit from fast, cheap models. Gemma 4 on Google AI's free tier is ideal.

**Verification Tier** — Low volume, reasoning focus:
- TechLead (requirement decomposition, planning)
- Senior (complex stories, code review, escalations)
- QA (plan-vs-execution verification, quality gate)
- Manager (failure diagnosis, corrective actions)

These roles need strong reasoning to catch issues. Claude Sonnet/Opus recommended.

## Default Configuration

VXD ships with a hybrid setup that maximizes free-tier usage:

| Role | Provider | Model | Tier |
|------|----------|-------|------|
| TechLead | anthropic | claude-opus-4 | Verification |
| Senior | anthropic | claude-sonnet-4 | Verification |
| Intermediate | google | gemma-4-27b-it | Execution |
| Junior | google | gemma-4-27b-it | Execution |
| QA | anthropic | claude-sonnet-4 | Verification |
| Supervisor | google | gemma-4-27b-it | Execution |
| Manager | anthropic | claude-sonnet-4 | Verification |

## Choosing Models

**Budget-conscious:** Use the defaults. Execution roles are free, verification roles use your Anthropic subscription or API key.

**Maximum quality:** Put all roles on Claude Sonnet or Opus. Higher cost but strongest reasoning across the board.

**Fully free:** Put all roles on Google/Gemma 4. Works for smaller projects but QA and Manager may miss subtle issues.

**OpenAI alternative:** Replace Anthropic roles with OpenAI equivalents (GPT-4o for verification, GPT-4o-mini for execution).

## Fallback Behavior

When a Google-configured role hits free-tier quota limits, VXD automatically falls back to Anthropic (Claude CLI or API). This is transparent — no manual intervention needed. See the [Gemma 4 Guide](gemma-4-guide.md) for details on quota limits and fallback chain.
