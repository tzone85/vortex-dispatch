# VXD Demo Tape
# Generates an animated GIF showing the VXD CLI in action.
#
# Prerequisites:
#   brew install vhs ffmpeg ttyd
#
# Usage:
#   vhs docs/demo.tape
#
# This produces docs/demo.gif.

# --- Configuration ---
Output docs/demo.gif
Set FontSize 14
Set Width 1200
Set Height 600
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 50ms
Set Shell "zsh"

# --- Scene 1: Show available commands ---
Type "echo '--- Step 1: Explore the VXD CLI ---'"
Enter
Sleep 1s

Type "vxd --help"
Enter
Sleep 3s

# --- Scene 2: Initialize VXD ---
Type "echo '--- Step 2: Initialize VXD in your project ---'"
Enter
Sleep 1s

Type "vxd init"
Enter
Sleep 3s

# --- Scene 3: Validate configuration ---
Type "echo '--- Step 3: Validate configuration ---'"
Enter
Sleep 1s

Type "vxd config validate"
Enter
Sleep 2s

Type "vxd config show"
Enter
Sleep 3s

# --- Scene 4: Check status ---
Type "echo '--- Step 4: Check the pipeline status ---'"
Enter
Sleep 1s

Type "vxd status"
Enter
Sleep 3s

# --- Scene 5: View events ---
Type "echo '--- Step 5: Review the event trail ---'"
Enter
Sleep 1s

Type "vxd events --limit 10"
Enter
Sleep 3s

# --- Scene 6: Launch the dashboard ---
Type "echo '--- Step 6: Launch the live TUI dashboard ---'"
Enter
Sleep 1s

Type "vxd dashboard"
Enter
Sleep 5s

# Exit dashboard
Type "q"
Sleep 1s

Type "echo 'Done! VXD orchestrates your AI agents from requirement to merged PR.'"
Enter
Sleep 3s
