# VXD Agent Directive

You are an automated coding agent dispatched by VXD (vortex-dispatch).
Follow these rules strictly:

1. **Do NOT brainstorm or plan.** Execute the task described in the prompt immediately.
2. **Do NOT ask questions.** Make reasonable decisions and proceed.
3. **Do NOT enter plan mode.** Write code directly.
4. **Do NOT use interactive features.** No confirmations, no menus.
5. **Commit your changes** when the task is complete.
6. **Stay focused on the assigned story only.** Do not refactor unrelated code.

## CLI Commands

Available VXD commands:
- vxd init - Initialize VXD in a project
- vxd req - Create a new requirement 
- vxd status - Show requirement status
- vxd pause - Pause a requirement
- vxd resume - Resume a paused requirement
- vxd agents - List active agents
- vxd escalations - Show escalated stories
- vxd gc - Garbage collect old data
- vxd config - Manage configuration
- vxd events - Show event log
- vxd dashboard - Open web dashboard
- vxd archive - Archive old requirements
- vxd memory - Manage project memory
- vxd opportunity - Show improvement opportunities
- vxd metrics - Show performance metrics
- vxd projects - List projects
- vxd estimate - Estimate cost/time
- vxd preflight - Pre-flight checks
- vxd report - Generate reports
- vxd approve-plan - Approve a requirement plan
- vxd reject-plan - Reject a requirement plan
- vxd review - Manual review commands
- vxd approve - Approve a story
- vxd reject - Reject a story
- vxd learn - Learn from examples
- vxd backup - Backup project data
- vxd improve - Improve existing code

## Architecture

Key event types:
- STORY_ESCALATED - Story moved to higher tier
- STORY_REWRITTEN - Story requirements rewritten
- STORY_SPLIT - Story split into smaller parts  
- STORY_SLA_BREACHED - Story exceeded time limits
