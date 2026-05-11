# Getting Started

This guide walks you through installing VXD and running your first initialization.

## Visual Overview

For a quick architectural picture, see the rendered diagrams under [`docs/diagrams/`](diagrams/):

- [Architecture overview](diagrams/arch-overview.svg) — services and event flow
- [Pipeline flow](diagrams/pipeline-flow.svg) — requirement → stories → merge
- [Escalation tiers](diagrams/escalation-tiers.svg) — 5-tier escalation chain
- [Package dependencies](diagrams/package-deps.svg) — Go package DAG
- [Sequence diagrams](diagrams/) — dispatch, escalation, merge, self-improve, autoresearch

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

# For Google AI Studio (free tier — used by default for Junior/Intermediate/Supervisor)
export GOOGLE_AI_API_KEY="your-key-here"

# For GitHub CLI (needed for PR creation)
gh auth login
```

> **Cost note:** The ANTHROPIC_API_KEY is only used for VXD's internal operations (a few API calls per story). The spawned coding agents — which do the heavy work — use your Claude Code subscription at no extra cost. If you only use OpenAI for internal operations, you don't need ANTHROPIC_API_KEY at all.

> **Gemma 4 default:** VXD uses Google AI Studio's free tier for execution roles (Junior, Intermediate, Supervisor) by default. Get a free API key from [Google AI Studio](https://aistudio.google.com/apikey). If no `GOOGLE_AI_API_KEY` is set, configure these roles to use `anthropic` or `openai` in `vxd.yaml`. See the [Model Selection Guide](model-selection.md) for details.

### MemPalace (Optional — Semantic Memory)

VXD supports [MemPalace](https://github.com/milla-jovovich/mempalace) for storing and retrieving institutional knowledge — decisions, debugging insights, architectural patterns — via semantic search. Completely local, zero API cost.

```bash
# Install
pip3 install mempalace

# Initialize and index the VXD codebase
python3 -m mempalace init /path/to/vortex-dispatch
python3 -m mempalace mine /path/to/vortex-dispatch

# Search your project's memory
python3 -m mempalace search "why did we choose event sourcing"

# Check what's indexed
python3 -m mempalace status
```

The self-improvement engine (`vxd-improve`) automatically re-mines the codebase after each run to keep the palace current.

## Before You Install: PATH Setup (required)

By default, `go install` places binaries in `~/go/bin/`. You can also build to `~/.local/bin/` if that is where your system resolves binaries. Either way, ensure the target directory is on your PATH.

### macOS / Linux

**1. Create the install directory:**

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
# Should show: /Users/<you>/go/bin (macOS) or /home/<you>/go/bin (Linux)
```

### Windows (PowerShell — without WSL)

If you prefer native Windows development without WSL, see the full [Windows Setup (WSL)](#windows-setup-wsl) section below. For a quick PowerShell PATH setup:

```powershell
# Add Go's bin directory to the current user's PATH
$gobin = Join-Path $env:USERPROFILE "go\bin"
[Environment]::SetEnvironmentVariable("PATH", "$gobin;$([Environment]::GetEnvironmentVariable('PATH', 'User'))", "User")
# Reload in this session
$env:PATH = "$gobin;$env:PATH"
```

> **Important:** VXD requires **tmux** for agent session management, which is only available on Unix. Windows users must use [WSL 2](#windows-setup-wsl) for the full pipeline. Native Windows works for `vxd status`, `vxd events`, and `vxd dashboard --web` (read-only commands) but not for `vxd req` or `vxd resume` which spawn tmux sessions.

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
| **Windows Terminal (recommended)** | Use [Windows Terminal](https://aka.ms/terminal) for better multi-tab WSL sessions, proper color support, and split panes for monitoring tmux sessions alongside VXD output |
| **Environment variables not persisting** | Add `export` lines to `~/.bashrc` inside WSL (not PowerShell). WSL does not inherit Windows environment variables unless configured via `/etc/wsl.conf` |
| **WSL memory limit** | VXD + multiple AI agents can use significant memory. If WSL runs out of memory, create `%UserProfile%\.wslconfig` with `[wsl2]\nmemory=8GB` (adjust to your system) |
| **Accessing WSL files from Windows** | Open File Explorer and navigate to `\\wsl$\Ubuntu\home\<you>\projects\` to browse WSL filesystem files from Windows |

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
| `database is locked` (SQLite) | Concurrent writes during pipeline | Fixed in latest version (WAL mode enabled). If upgrading, delete `~/.vxd/vxd.db` and replay events |
| **WSL:** `localhost` not reachable from Windows | WSL 2 networking issue | Run `wsl --shutdown` from PowerShell and restart WSL. Alternatively, check `ip addr show eth0` in WSL and use that IP |
| **WSL:** `npm: command not found` | Node.js not installed in WSL | Install Node.js inside WSL (not Windows Node.js): `curl -fsSL https://deb.nodesource.com/setup_22.x \| sudo -E bash - && sudo apt install -y nodejs` |
| **WSL:** file permissions wrong | NTFS mount permissions | Add to `/etc/wsl.conf`: `[automount]\noptions = "metadata"` then restart WSL |

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

## Porting to a New Machine (Mac Mini, second laptop, etc.)

Complete setup for a fresh macOS machine in ~15 minutes.

### Step 1: Install Prerequisites

```bash
# Homebrew (if not installed)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Core tools
brew install go tmux gh python3
npm install -g @anthropic-ai/claude-code

# PATH setup
mkdir -p ~/.local/bin
echo 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### Step 2: Clone and Build

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
go build -o ~/.local/bin/vxd ./cmd/vxd
go build -o ~/.local/bin/vxd-improve ./cmd/vxd-improve
```

### Step 3: Authenticate

```bash
# Claude Code (uses your Max subscription — no API costs for agents)
claude login

# GitHub CLI (for PR creation)
gh auth login

# API keys — add these to ~/.zshrc
cat >> ~/.zshrc << 'EOF'
export GOOGLE_AI_API_KEY="your-google-ai-key"
export FIRECRAWL_API_KEY="your-firecrawl-key"
export RESEND_API_KEY="your-resend-key"
EOF
source ~/.zshrc
```

### Step 4: Initialize VXD

```bash
cd vortex-dispatch
vxd init
```

### Step 5: Set Up Self-Improvement Engine

```bash
# Create log directory
mkdir -p ~/.vxd/self-improve

# Install the launchd schedule (daily at 6am)
cat > ~/Library/LaunchAgents/com.vxd.self-improve.plist << 'PLISTEOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.vxd.self-improve</string>
    <key>ProgramArguments</key>
    <array>
        <string>~/.local/bin/vxd-improve</string>
    </array>
    <key>WorkingDirectory</key>
    <string>~/Sites/misc/vortex-dispatch</string>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key><integer>6</integer>
        <key>Minute</key><integer>0</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>~/.vxd/self-improve/launchd.log</string>
    <key>StandardErrorPath</key>
    <string>~/.vxd/self-improve/launchd.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>~/.local/bin:~/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
        <key>HOME</key>
        <string>~</string>
    </dict>
</dict>
</plist>
PLISTEOF

# IMPORTANT: Edit the plist and replace ~ with your actual home directory path
# e.g., /Users/yourusername
sed -i '' "s|~|$HOME|g" ~/Library/LaunchAgents/com.vxd.self-improve.plist

# Load the schedule
launchctl load ~/Library/LaunchAgents/com.vxd.self-improve.plist

# Test it works
vxd-improve --dry-run
```

### Step 6: Set Up MemPalace (Optional)

```bash
pip3 install mempalace
cd vortex-dispatch

# Initialize (accept defaults at each prompt by pressing Enter)
python3 -m mempalace init .

# Index the codebase
python3 -m mempalace mine .

# Verify
python3 -m mempalace search "event sourcing"
```

### Step 7: Verify Everything

```bash
vxd --help                    # CLI works
vxd memory --web              # Timeline + findings dashboard opens in browser
vxd-improve --dry-run         # Self-improvement engine runs
python3 -m mempalace status   # MemPalace indexed
launchctl list | grep vxd     # Daily schedule active
```

### What Runs Automatically

| Schedule | What | Log |
|----------|------|-----|
| Daily 6am | `vxd-improve` — research, analyze, implement, email, re-mine MemPalace | `~/.vxd/self-improve/launchd.log` |

Reports sent to `vortex.dispatch01@gmail.com`.

## Next Steps

You're ready to explore the full pipeline. Head to the [Tutorial](tutorial.md) for a detailed walkthrough.
