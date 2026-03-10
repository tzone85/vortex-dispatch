# Getting Started

This guide walks you through installing VXD and running your first initialization.

## Prerequisites

Before installing VXD, ensure you have the following tools available:

| Tool | Purpose | Install |
|------|---------|---------|
| **Go 1.23+** | Build and install VXD | [go.dev/dl](https://go.dev/dl/) |
| **tmux** | Agent session management | `brew install tmux` / `apt install tmux` |
| **GitHub CLI (gh)** | PR creation and auto-merge | `brew install gh` / [cli.github.com](https://cli.github.com) |
| **SQLite3** | State projection storage | Usually pre-installed on macOS/Linux |

You also need at least one AI runtime CLI installed:

| Runtime | Install | Models |
|---------|---------|--------|
| **Claude Code** | `npm install -g @anthropic-ai/claude-code` | Opus 4, Sonnet 4, Haiku 4 |
| **Codex** | `npm install -g @openai/codex` | o3, o4-mini |
| **Gemini CLI** | `npm install -g @google/gemini-cli` | Gemini 2.5 Pro, Flash |

### API Keys

Set the appropriate environment variables for your chosen providers:

```bash
# For Anthropic models (Claude Code, default for most roles)
export ANTHROPIC_API_KEY="sk-ant-..."

# For OpenAI models (Codex, or if using OpenAI for junior role)
export OPENAI_API_KEY="sk-..."

# For GitHub CLI (needed for PR creation)
gh auth login
```

## Installation

### From Source (recommended for contributors)

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
make build
make install   # Copies vxd to $GOPATH/bin
```

### Via Go Install

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
```

### Verify Installation

```bash
vxd --help
```

You should see the full command list: `init`, `req`, `status`, `resume`, `agents`, `escalations`, `gc`, `config`, `events`, `dashboard`.

## First Run: `vxd init`

Initialize your workspace from within any git repository:

```bash
cd ~/my-project
vxd init
```

This creates:

```
~/.vxd/
  events.jsonl       # Append-only event log
  vxd.db             # SQLite projection store
  config/            # Default configuration
```

And copies `vxd.config.example.yaml` to `vxd.yaml` in your project root if one doesn't exist.

## Verify Your Setup

Run a quick config validation:

```bash
vxd config validate
```

If everything is configured correctly, you'll see a success message. Common issues:

| Error | Fix |
|-------|-----|
| `tmux not found` | Install tmux: `brew install tmux` |
| `gh not found` | Install GitHub CLI and run `gh auth login` |
| `config not found` | Run `vxd init` or copy `vxd.config.example.yaml` to `vxd.yaml` |
| `ANTHROPIC_API_KEY not set` | Export your API key in your shell profile |

## Next Steps

You're ready to submit your first requirement. Head to the [Tutorial](tutorial.md) for a hands-on walkthrough.
