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

### Authentication

VXD uses two authentication paths:

**1. Spawned agents (Claude Code, Codex, Gemini CLI)** — authenticate via their own built-in sessions. For Claude Code, this means your existing subscription (Max/Pro) logged in via `claude login`. No API key needed — agents run at no additional cost beyond your subscription.

**2. VXD's internal operations (planning, code review, QA)** — use API keys for direct LLM calls. These are lightweight (one call per story per stage), so API usage is minimal.

```bash
# Authenticate Claude Code CLI (uses your Max/Pro subscription)
claude login

# API key for VXD's internal planner/reviewer/QA calls
export ANTHROPIC_API_KEY="sk-ant-..."

# For OpenAI models (Codex runtime, or if using OpenAI for planner)
export OPENAI_API_KEY="sk-..."

# For GitHub CLI (needed for PR creation)
gh auth login
```

> **Cost note:** The ANTHROPIC_API_KEY is only used for VXD's internal operations (a few API calls per story). The spawned coding agents — which do the heavy work — use your Claude Code subscription at no extra cost. If you only use OpenAI for internal operations, you don't need ANTHROPIC_API_KEY at all.

## Before You Install: PATH Setup (required)

The VXD binary installs to `~/go/bin/`. You **must** ensure this directory exists and is on your PATH before proceeding.

**1. Create the Go bin directory:**

```bash
mkdir -p "$(go env GOPATH)/bin"
```

**2. Add it to your PATH** by appending this line to `~/.zshrc` (or `~/.bash_profile` for Bash):

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
```

**3. Reload your shell:**

```bash
source ~/.zshrc
```

**4. Verify** the directory is on your PATH:

```bash
echo $PATH | tr ':' '\n' | grep go/bin
# Should show: /Users/<you>/go/bin
```

> **Note:** Windows setup may differ — refer to the [Go installation docs](https://go.dev/doc/install) for platform-specific guidance on configuring `GOPATH` and `PATH`.

## Installation

### From Source (recommended)

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
make build && make install
```

### Via Go Install

> **Note:** This only works if the repository is public or you have configured `GOPRIVATE`. See [Private Repos](#private-repo-setup) below.

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
```

### Verify Installation

```bash
vxd --help
```

You should see the full command list: `init`, `req`, `status`, `resume`, `agents`, `escalations`, `gc`, `config`, `events`, `dashboard`. The `dashboard` command accepts `--web` and `--port` flags for the browser-based dashboard.

If you see `zsh: command not found: vxd`, go back to the [PATH setup](#before-you-install-path-setup-required) section.

### Private Repo Setup

If the repository is private, `go install` won't work through the public Go module proxy. Either build from source (recommended), or configure Go for private repos:

```bash
# Tell Go to bypass the public proxy for your repos
echo 'export GOPRIVATE=github.com/tzone85/*' >> ~/.zshrc
source ~/.zshrc

# Ensure git can authenticate via HTTPS
gh auth setup-git
```

## Setting Up a New Project

Once VXD is installed, you can use it in **any** git repository. You do not need to be inside the vortex-dispatch source directory.

### Step 1: Create or navigate to your project

```bash
mkdir ~/my-project
cd ~/my-project
git init
```

### Step 2: Initialize the workspace

```bash
vxd init
```

This creates the global state directory and generates a `vxd.yaml` config file in your project root with sensible defaults:

```
~/.vxd/
  events.jsonl       # Append-only event log
  vxd.db             # SQLite projection store
```

Customize `vxd.yaml` as needed — see [Configuration](configuration.md) for all available options.

### Step 3: Validate your setup

```bash
vxd config validate
```

If everything is configured correctly, you'll see a success message. Common issues:

| Error | Fix |
|-------|-----|
| `command not found: vxd` | Complete the [PATH setup](#before-you-install-path-setup-required) |
| `tmux not found` | Install tmux: `brew install tmux` |
| `gh not found` | Install GitHub CLI and run `gh auth login` |
| `config not found` | Run `vxd init` in your project directory to generate `vxd.yaml` |
| `ANTHROPIC_API_KEY not set` | Only needed for VXD's internal operations (planner, reviewer, QA). Export it in your shell profile. Spawned agents use your Claude subscription instead. |

### Step 4: Submit your first requirement

```bash
vxd req "Build a REST API for user management with CRUD endpoints"
vxd status
vxd dashboard           # single-pane TUI (j/k scroll stories, w open web, q quit)
vxd dashboard --web     # browser-based dashboard at localhost:8787
```

## Generating the Demo GIF (optional)

If you want to generate the animated demo GIF, you'll need [VHS](https://github.com/charmbracelet/vhs) along with its dependencies `ffmpeg` and `ttyd`. On macOS:

```bash
brew install vhs ffmpeg ttyd
vhs docs/demo.tape
```

This produces `docs/demo.gif`.

## Next Steps

You're ready to explore the full pipeline. Head to the [Tutorial](tutorial.md) for a detailed walkthrough.
