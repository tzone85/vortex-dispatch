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

> **Windows users:** See [Windows Setup (WSL)](#windows-setup-wsl) below for detailed instructions.

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

## Windows Setup (WSL)

VXD uses **tmux** for agent session management, which is a Unix-only tool. Windows users must run VXD inside [WSL 2](https://learn.microsoft.com/en-us/windows/wsl/install) (Windows Subsystem for Linux).

### Step 1: Install WSL 2

Open **PowerShell as Administrator** and run:

```powershell
wsl --install -d Ubuntu
```

Restart your machine when prompted. After reboot, the Ubuntu terminal opens automatically — create your Unix username and password.

### Step 2: Install prerequisites inside WSL

Open your WSL terminal (search "Ubuntu" in Start menu) and install all dependencies:

```bash
# System tools
sudo apt update && sudo apt install -y tmux sqlite3 git curl build-essential

# Go (check https://go.dev/dl/ for latest version)
wget https://go.dev/dl/go1.24.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz
echo 'export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc

# GitHub CLI
(type -p wget >/dev/null || sudo apt install wget -y) \
  && sudo mkdir -p -m 755 /etc/apt/keyrings \
  && out=$(mktemp) && wget -nv -O$out https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  && cat $out | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null \
  && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
  && sudo apt update \
  && sudo apt install gh -y

# Node.js (for Claude Code CLI)
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt install -y nodejs

# Claude Code CLI
npm install -g @anthropic-ai/claude-code
```

### Step 3: Authenticate

```bash
claude login          # Claude Code subscription (Max/Pro)
gh auth login         # GitHub CLI
export ANTHROPIC_API_KEY="sk-ant-..."   # For VXD internal LLM calls
```

Add the API key export to `~/.bashrc` so it persists across sessions:

```bash
echo 'export ANTHROPIC_API_KEY="sk-ant-..."' >> ~/.bashrc
```

### Step 4: Build and install VXD

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
make build && make install
vxd --help    # Verify installation
```

### Step 5: Use VXD

All VXD commands run inside WSL. Navigate to your project directory:

```bash
# Access your Windows files from WSL
cd /mnt/c/Users/<YourName>/Projects/my-app

# Or work in the WSL filesystem (faster I/O)
mkdir -p ~/projects/my-app && cd ~/projects/my-app
git init

vxd init
vxd req "Build a REST API for user management"
vxd dashboard
```

### Web Dashboard on Windows

`vxd dashboard --web` starts an HTTP server inside WSL. Your Windows browser can access it at the same `localhost` URL:

```bash
vxd dashboard --web --port 8787
# Open http://localhost:8787 in your Windows browser
```

WSL 2 automatically forwards localhost ports to Windows, so no extra configuration is needed.

### Windows Tips

| Scenario | Solution |
|----------|----------|
| **Slow file I/O on `/mnt/c/`** | Work in the WSL filesystem (`~/projects/`) instead of the Windows mount. Git operations are 3-10x faster. |
| **tmux not found** | Run `sudo apt install tmux` inside WSL |
| **Browser doesn't open automatically** | Copy the URL from the terminal and paste it into your Windows browser |
| **VS Code integration** | Install the [WSL extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-wsl), then `code .` from WSL opens VS Code connected to your WSL filesystem |
| **Git credential sharing** | Run `git config --global credential.helper "/mnt/c/Program\ Files/Git/mingw64/bin/git-credential-manager.exe"` to share Windows Git credentials with WSL |

## Troubleshooting

### Common Issues

| Problem | Cause | Solution |
|---------|-------|----------|
| `vxd: command not found` | `~/go/bin` not in PATH | Complete the [PATH setup](#before-you-install-path-setup-required) section |
| `no LLM available` | No API key and no Claude CLI | Set `ANTHROPIC_API_KEY` or install Claude Code CLI (`npm install -g @anthropic-ai/claude-code`) |
| `tmux not found` | tmux not installed | Install: `brew install tmux` (macOS) or `sudo apt install tmux` (Ubuntu/WSL) |
| `gh not found` | GitHub CLI not installed | Install: `brew install gh` (macOS) or see [cli.github.com](https://cli.github.com) |
| `repository has no commits` | Empty git repo | Run `git add . && git commit -m "initial commit"` before `vxd resume` |
| `planning failed: prompt too large` | Requirement too long for CLI mode | Set `ANTHROPIC_API_KEY` for API-based planning, or split the requirement |
| Agent stuck in permission prompt | Runtime permission denied | Use `--godmode` flag or set `planning.godmode: true` in vxd.yaml |
| `config not found` | Missing vxd.yaml | Run `vxd init` in your project directory |
| Agent sessions invisible | Wrong tmux server | Run `tmux list-sessions` to verify sessions exist |
| Merge fails with conflicts | Parallel agents touched same files | VXD uses LLM-powered conflict resolution; if it fails repeatedly, try reducing parallel stories |

### Verifying Your Setup

Run this checklist before your first requirement:

```bash
# 1. Check VXD is installed
vxd --help

# 2. Check configuration is valid
vxd config validate

# 3. Check required tools
which tmux && which gh && which git

# 4. Check at least one AI runtime
which claude || which codex || which gemini

# 5. Check GitHub CLI auth
gh auth status

# 6. Check repo has at least one commit
git log --oneline -1
```

### Getting Help

- **Events log**: `vxd events --limit 20` shows what happened
- **Agent sessions**: `tmux list-sessions` shows active agent sessions
- **Agent output**: `tmux capture-pane -t <session-name> -p | tail -30` shows what an agent is doing
- **Config check**: `vxd config show` prints the active configuration

## Next Steps

You're ready to explore the full pipeline. Head to the [Tutorial](tutorial.md) for a detailed walkthrough.
