package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderEscalations renders active and recent escalations.
func (m Model) renderEscalations(width, maxRows int) string {
	heading := headingStyle.Render("Escalations")
	escalations := m.escalations
	if len(escalations) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			heading,
			lipgloss.NewStyle().Foreground(colorGray).Render("  No escalations recorded."),
		)
	}

	// Summary counts.
	pending := 0
	resolved := 0
	for _, e := range escalations {
		switch e.Status {
		case "pending":
			pending++
		case "resolved":
			resolved++
		}
	}

	summary := fmt.Sprintf("Pending: %s  |  Resolved: %s",
		escalationPendingStyle.Render(fmt.Sprintf("%d", pending)),
		escalationResolvedStyle.Render(fmt.Sprintf("%d", resolved)),
	)

	// Column widths.
	colStory := 20
	colFrom := 16
	colStatus := 10
	colTier := 10
	colTime := 20
	colReason := max(width-colStory-colFrom-colStatus-colTier-colTime-14, 10)

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
		colStory, "STORY",
		colFrom, "FROM",
		colStatus, "STATUS",
		colTier, "TIER",
		colTime, "CREATED",
		colReason, "REASON",
	)

	separator := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
		colStory, strings.Repeat("─", colStory-1),
		colFrom, strings.Repeat("─", colFrom-1),
		colStatus, strings.Repeat("─", colStatus-1),
		colTier, strings.Repeat("─", colTier-1),
		colTime, strings.Repeat("─", colTime-1),
		colReason, strings.Repeat("─", colReason-1),
	)

	var lines []string
	lines = append(lines, headingStyle.Render(summary))
	lines = append(lines, "")
	lines = append(lines, columnHeaderStyle.Render(header))
	lines = append(lines, lipgloss.NewStyle().Foreground(colorDimGray).Render(separator))

	rowLimit := maxRows - 6
	if rowLimit < 1 {
		rowLimit = 10
	}

	for i, e := range escalations {
		if i >= rowLimit {
			remaining := len(escalations) - rowLimit
			if remaining > 0 {
				lines = append(lines, lipgloss.NewStyle().Foreground(colorGray).Render(
					fmt.Sprintf("  ... and %d more", remaining),
				))
			}
			break
		}

		storyID := e.StoryID
		if storyID == "" {
			storyID = "-"
		}

		var statusStyle lipgloss.Style
		if e.Status == "pending" {
			statusStyle = escalationPendingStyle
		} else {
			statusStyle = escalationResolvedStyle
		}

		tier := fmt.Sprintf("Tier %d→%d", e.FromTier, e.ToTier)
		row := fmt.Sprintf("  %-*s %-*s %s %-*s %-*s %-*s",
			colStory, truncateStr(storyID, colStory-1),
			colFrom, truncateStr(e.FromAgent, colFrom-1),
			statusStyle.Render(fmt.Sprintf("%-*s", colStatus, e.Status)),
			colTier, truncateStr(tier, colTier-1),
			colTime, truncateStr(e.CreatedAt, colTime-1),
			colReason, truncateStr(e.Reason, colReason-1),
		)

		lines = append(lines, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(lines, "\n"))
}
