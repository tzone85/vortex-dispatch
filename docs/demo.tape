# VXD Demo Tape
# Generates an animated GIF showing the full VXD workflow:
# vxd init -> vxd req -> vxd status -> vxd dashboard
#
# Prerequisites:
#   brew install vhs   (or go install github.com/charmbracelet/vhs@latest)
#
# Usage:
#   vhs docs/demo.tape
#
# This produces demo.gif in the current directory.

# --- Configuration ---
Output docs/demo.gif
Set FontSize 14
Set Width 1200
Set Height 600
Set Theme "Dracula"
Set Padding 20
Set TypingSpeed 50ms
Set Shell "zsh"

# --- Scene 1: Initialize VXD ---
Type "# Step 1: Initialize VXD in your project"
Enter
Sleep 1s

Type "vxd init"
Enter
Sleep 3s

# --- Scene 2: Submit a requirement ---
Type "# Step 2: Submit a natural-language requirement"
Enter
Sleep 1s

Type@100ms 'vxd req "Add a health check endpoint at GET /healthz that returns 200 OK with service name, version, and uptime"'
Enter
Sleep 5s

# --- Scene 3: Check status ---
Type "# Step 3: Check the pipeline status"
Enter
Sleep 1s

Type "vxd status"
Enter
Sleep 3s

# --- Scene 4: View agents ---
Type "# Step 4: See which agents are assigned"
Enter
Sleep 1s

Type "vxd agents"
Enter
Sleep 3s

# --- Scene 5: View recent events ---
Type "# Step 5: Review the event trail"
Enter
Sleep 1s

Type "vxd events --limit 10"
Enter
Sleep 3s

# --- Scene 6: Launch the dashboard ---
Type "# Step 6: Launch the live TUI dashboard"
Enter
Sleep 1s

Type "vxd dashboard"
Enter
Sleep 5s

# Exit dashboard
Type "q"
Sleep 1s

Type "# Done! VXD orchestrates your AI agents from requirement to merged PR."
Enter
Sleep 3s
