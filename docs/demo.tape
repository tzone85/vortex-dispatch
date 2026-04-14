# VXD Demo — Full Pipeline Tutorial
# Shows: preflight → req → approve-plan → resume → dashboard → merged PR
#
# Prerequisites:
#   brew install vhs ffmpeg ttyd
#   go build -o ~/.local/bin/vxd ./cmd/vxd
#   unset ANTHROPIC_API_KEY  (if using Max subscription)
#
# Usage:
#   cd ~/Sites/misc/vxd-dry-run && vhs ~/Sites/misc/vortex-dispatch/docs/demo.tape
#
# This produces docs/demo.gif (~30s recording).
# Run from a test project repo (e.g., vxd-dry-run) with a GitHub remote.
#
# Cleanup after recording:
#   vxd gc && rm -rf ~/.vxd/projects/vxd-dry-run/

# --- Configuration ---
Output docs/demo.gif
Set FontSize 14
Set Width 1200
Set Height 700
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 35ms
Set Shell "zsh"

# --- Scene 1: Preflight — Check environment ---
Type "echo '── Step 1: Preflight Check ──'"
Enter
Sleep 500ms

Type "vxd preflight"
Enter
Sleep 4s

# --- Scene 2: Submit a requirement ---
Type "echo '── Step 2: Submit a Requirement ──'"
Enter
Sleep 500ms

Type@60ms 'vxd req "Add a Delete(id string) error method to the Store in internal/store/store.go. Return error if not found. Add tests."'
Enter
Sleep 20s

# --- Scene 3: Review the plan ---
Type "echo '── Step 3: Review the Plan ──'"
Enter
Sleep 500ms

Type "vxd status"
Enter
Sleep 3s

# --- Scene 4: Approve the plan ---
Type "echo '── Step 4: Approve the Plan ──'"
Enter
Sleep 500ms

# Use a placeholder — actual req-id will differ per run
Type "# vxd approve-plan <req-id>"
Enter
Sleep 2s

# --- Scene 5: Resume — dispatch agents ---
Type "echo '── Step 5: Dispatch Agents ──'"
Enter
Sleep 500ms

Type "# vxd resume <req-id> --auto"
Enter
Type "echo 'Agents spawn in tmux sessions, work autonomously...'"
Enter
Sleep 3s

# --- Scene 6: Monitor via dashboard ---
Type "echo '── Step 6: Monitor via Dashboard ──'"
Enter
Sleep 500ms

Type "echo 'TUI:  vxd dashboard'"
Enter
Type "echo 'Web:  vxd dashboard --web'"
Enter
Sleep 2s

Type "vxd dashboard"
Enter
Sleep 8s

# Exit dashboard
Type "q"
Sleep 1s

# --- Scene 7: Check results ---
Type "echo '── Step 7: Results ──'"
Enter
Sleep 500ms

Type "vxd status"
Enter
Sleep 4s

Type "vxd metrics"
Enter
Sleep 4s

# --- Scene 8: Cleanup ---
Type "echo '── Step 8: Cleanup ──'"
Enter
Sleep 500ms

Type "vxd gc --dry-run"
Enter
Sleep 3s

# --- Wrap up ---
Type "echo ''"
Enter
Type "echo 'VXD: Hand off a requirement, walk away, come back to merged PRs.'"
Enter
Type "echo ''"
Enter
Type "echo 'Full docs: https://github.com/tzone85/vortex-dispatch'"
Enter
Sleep 5s
