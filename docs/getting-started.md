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

#### macOS: Additional Setup

On macOS you may need to complete these extra steps before `make install` and `vxd` will work:

**1. Create the Go bin directory** (if it doesn't already exist):

```bash
mkdir -p "$(go env GOPATH)/bin"
```

**2. Add it to your PATH** by appending this line to `~/.zshrc` (or `~/.bash_profile` for Bash):

```bash
export PATH="$HOME/go/bin:$PATH"
```

**3. Reload your shell** so the PATH change takes effect:

```bash
source ~/.zshrc
```

Then re-run `make install` and proceed to verification below.

> **Note:** Windows setup may differ -- refer to the [Go installation docs](https://go.dev/doc/install) for platform-specific guidance on configuring `GOPATH` and `PATH`.

### Via Go Install

```bash
go install github.com/tzone85/vortex-dispatch/cmd/vxd@latest
```

### Verify Installation

```bash
vxd --help
```

You should see the full command list: `init`, `req`, `status`, `resume`, `agents`, `escalations`, `gc`, `config`, `events`, `dashboard`.

## Configuration

VXD requires a `vxd.yaml` config file in your project root. You can either let `vxd init` create it for you (see below), or copy it manually:

```bash
cp vxd.config.example.yaml vxd.yaml
```

Customize it as needed -- see [Configuration](configuration.md) for all available options.

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

It also copies `vxd.config.example.yaml` to `vxd.yaml` in your project root if one doesn't already exist.

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

## Generating the Demo GIF (optional)

If you want to generate the animated demo GIF, you'll need [VHS](https://github.com/charmbracelet/vhs) along with its dependencies `ffmpeg` and `ttyd`. On macOS:

```bash
brew install vhs ffmpeg ttyd
vhs docs/demo.tape
```

This produces `docs/demo.gif`.

## Next Steps

You're ready to submit your first requirement. Head to the [Tutorial](tutorial.md) for a hands-on walkthrough.
