# VXD Demo Tape
# Generates an animated GIF showing the full VXD pipeline in action:
# init -> config -> req (with live decomposition) -> status -> events -> dashboard
#
# Prerequisites:
#   brew install vhs ffmpeg ttyd
#
# Usage:
#   vhs docs/demo.tape
#
# This produces docs/demo.gif.
#
# NOTE: The `vxd req` command will actually submit a requirement and trigger
# Tech Lead decomposition. To clean up afterwards:
#   vxd gc --dry-run   # see what would be removed
#   vxd gc             # remove worktrees and branches
#   rm -rf ~/.vxd      # full reset (deletes all state)
#   rm nxd.yaml        # if config was created by init

# --- Configuration ---
Output docs/demo.gif
Set FontSize 14
Set Width 1200
Set Height 700
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 40ms
Set Shell "zsh"

# --- Scene 1: Show available commands ---
Type "echo '=== Step 1: Explore the VXD CLI ==='"
Enter
Sleep 1s

Type "vxd --help"
Enter
Sleep 4s

# --- Scene 2: Initialize VXD ---
Type "echo '=== Step 2: Initialize VXD workspace ==='"
Enter
Sleep 1s

Type "vxd init"
Enter
Sleep 3s

# --- Scene 3: Validate and show configuration ---
Type "echo '=== Step 3: Validate configuration ==='"
Enter
Sleep 1s

Type "vxd config validate"
Enter
Sleep 2s

Type "vxd config show"
Enter
Sleep 4s

# --- Scene 4: Submit a real requirement ---
Type "echo '=== Step 4: Submit a requirement (Tech Lead decomposes into stories) ==='"
Enter
Sleep 1s

Type@80ms 'vxd req "Build a REST API for user management with CRUD endpoints"'
Enter
Sleep 15s

# --- Scene 5: Check pipeline status ---
Type "echo '=== Step 5: Check pipeline status ==='"
Enter
Sleep 1s

Type "vxd status"
Enter
Sleep 4s

# --- Scene 6: View agents ---
Type "echo '=== Step 6: View assigned agents ==='"
Enter
Sleep 1s

Type "vxd agents"
Enter
Sleep 3s

# --- Scene 7: Review the event trail ---
Type "echo '=== Step 7: Review the event trail ==='"
Enter
Sleep 1s

Type "vxd events --limit 20"
Enter
Sleep 4s

# --- Scene 8: Check for escalations ---
Type "echo '=== Step 8: Check escalations ==='"
Enter
Sleep 1s

Type "vxd escalations"
Enter
Sleep 3s

# --- Scene 9: Launch the live TUI dashboard ---
Type "echo '=== Step 9: Launch the live TUI dashboard ==='"
Enter
Sleep 1s

Type "vxd dashboard"
Enter
Sleep 8s

# Exit dashboard
Type "q"
Sleep 1s

# --- Wrap up ---
Type "echo ''"
Enter
Type "echo 'Done! VXD decomposed the requirement into stories, assigned agents,'"
Enter
Type "echo 'and is orchestrating the full pipeline: plan -> dispatch -> execute -> review -> QA -> merge'"
Enter
Type "echo ''"
Enter
Type "echo 'Cleanup: run \"vxd gc\" to remove worktrees/branches, or \"rm -rf ~/.vxd\" for a full reset.'"
Enter
Sleep 5s
