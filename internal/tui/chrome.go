package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cockpit/internal/build"
)

// renderTopBar returns the persistent single-row top bar spanning width columns.
// At narrow widths it progressively drops lower-priority fields so the bar
// always fits on a single line without wrapping.
func renderTopBar(m Model, width int) string {
	totalCost := 0.0
	for _, p := range m.projects {
		totalCost += p.LastCost
	}
	agentCount := len(m.processes)
	busyCount := 0
	for _, p := range m.processes {
		if p.Status == "busy" {
			busyCount++
		}
	}
	recentModel := "─"
	if len(m.sessions) > 0 && m.sessions[0].Model != "" {
		recentModel = truncate(m.sessions[0].Model, 20)
	}

	accent := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	primary := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colorTextDim)
	muted := lipgloss.NewStyle().Foreground(colorTextMuted)
	cost := lipgloss.NewStyle().Foreground(colorCost)

	// Left: brand · model (model dropped below 90 cols).
	left := "  " + accent.Render("◈") + "  " + primary.Render("cockpit") +
		muted.Render("  /  ") + dim.Render("claude-code")
	if width >= 90 {
		left += "   " + muted.Render("|") + "   " + dim.Render(truncate(recentModel, 20))
	}

	// Right: build from most- to least-important; drop fields as width shrinks.
	var statusPill string
	if busyCount > 0 {
		statusPill = accent.Render(frame(pulseFrames, m.animFrame)+" executing") +
			muted.Render(fmt.Sprintf("  %d/%d", busyCount, agentCount))
	} else {
		statusPill = muted.Render("○  idle")
	}

	right := statusPill + "   " + muted.Render("|") + "   " +
		cost.Render(fmt.Sprintf("$%.2f", totalCost))
	if width >= 110 {
		right += "   " + dim.Render(fmt.Sprintf("%d agents", agentCount)) +
			"   " + dim.Render(fmt.Sprintf("%d projects", len(m.projects)))
	}
	right += "  "

	// Use width-1 as the content target: the outer Width(width) pads the last
	// column, preventing the terminal EOL-wrap edge case on exact-width lines.
	avail := width - 1
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := avail - lw - rw
	if gap < 1 {
		// Too tight — collapse right to just cost.
		right = cost.Render(fmt.Sprintf("$%.2f", totalCost)) + " "
		rw = lipgloss.Width(right)
		gap = avail - lw - rw
		if gap < 1 {
			gap = 1
		}
	}

	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().Width(width).Background(colorPanel).Render(line)
}

// renderTabStrip returns the one-row tab strip spanning width columns.
func renderTabStrip(m Model, width int) string {
	type tab struct {
		v     viewName
		label string
	}
	tabs := []tab{
		{viewDashboard, "dashboard"},
		{viewAgents, "agents"},
		{viewTests, "tests"},
		{viewSessions, "sessions"},
	}

	activeStyle := lipgloss.NewStyle().
		Foreground(colorAccent).
		Background(colorBorderActive).
		Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(colorTextMuted)
	sepStyle := lipgloss.NewStyle().Foreground(colorBorder)

	var sb strings.Builder
	sb.WriteString("  ")
	for i, t := range tabs {
		if i > 0 {
			sb.WriteString(sepStyle.Render("  ·  "))
		}
		label := " " + t.label + " "
		if t.v == m.view {
			sb.WriteString(activeStyle.Render(label))
		} else {
			sb.WriteString(inactiveStyle.Render(label))
		}
	}

	return lipgloss.NewStyle().Width(width).Background(colorPanelAlt).Render(sb.String())
}

// renderStatusBar returns the persistent single-row bottom status bar.
// At narrow widths the path is shortened or dropped so it fits on one line.
func renderStatusBar(m Model, width int) string {
	agentCount := len(m.processes)
	totalCost := 0.0
	for _, p := range m.projects {
		totalCost += p.LastCost
	}

	accent := lipgloss.NewStyle().Foreground(colorAccent)
	dim := lipgloss.NewStyle().Foreground(colorTextDim)
	muted := lipgloss.NewStyle().Foreground(colorTextMuted)
	costStyle := lipgloss.NewStyle().Foreground(colorCost)

	left := "  " + accent.Render("●") + "  " + muted.Render("cockpit.loop")
	if width >= 90 {
		left += "   " + dim.Render(fmt.Sprintf("agents %d", agentCount))
	}
	if width >= 100 {
		left += "   " + dim.Render(fmt.Sprintf("projects %d", len(m.projects)))
	}
	left += "   " + costStyle.Render(fmt.Sprintf("$%.2f", totalCost))

	ver := dim.Render("v" + build.Version)

	// Path length scales down with terminal width.
	pathLen := 28
	if width < 120 {
		pathLen = 20
	}
	if width < 100 {
		pathLen = 12
	}
	right := muted.Render(truncate(m.claudeDir, pathLen)) + "   " + ver + "  "

	// Target width-1 to avoid terminal EOL-wrap on exact-width lines.
	avail := width - 1
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	dotCount := avail - lw - rw - 4

	// Doesn't fit — drop path, keep only version.
	if dotCount < 3 {
		right = ver + "  "
		rw = lipgloss.Width(right)
		dotCount = avail - lw - rw - 4
	}
	if dotCount < 1 {
		dotCount = 1
	}

	line := left + "  " + muted.Render(strings.Repeat("·", dotCount)) + "  " + right
	return lipgloss.NewStyle().Width(width).Background(colorPanel).Render(line)
}
