package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cockpit/internal/build"
)

// renderTopBar returns the persistent single-row top bar spanning width columns.
//
// Layout (left → right):
//
//	◈  cockpit  /  claude-code  |  <model>  ···  ○ idle | $X today  N agents  M projects
func renderTopBar(m Model, width int) string {
	// Aggregate stats from model state.
	totalCost := 0.0
	for _, p := range m.projects {
		totalCost += p.LastCost
	}
	agentCount := 0
	if m.manager != nil {
		agentCount = len(m.manager.List())
	}
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

	// Left segment: brand glyph · product · divider · recent model.
	left := "  " +
		accent.Render("◈") + "  " +
		primary.Render("cockpit") +
		muted.Render("  /  ") +
		dim.Render("claude-code") +
		"   " + muted.Render("|") + "   " +
		dim.Render(recentModel)

	// Right segment: animated status pill · telemetry.
	var statusPill string
	if busyCount > 0 {
		dot := frame(pulseFrames, m.animFrame)
		statusPill = accent.Render(dot+" executing") +
			muted.Render(fmt.Sprintf("  %d/%d", busyCount, agentCount))
	} else {
		statusPill = muted.Render("○  idle")
	}
	right := statusPill +
		"   " + muted.Render("|") + "   " +
		cost.Render(fmt.Sprintf("$%.2f today", totalCost)) +
		"   " + dim.Render(fmt.Sprintf("%d agents", agentCount)) +
		"   " + dim.Render(fmt.Sprintf("%d projects", len(m.projects))) +
		"  "

	// Fill the gap between left and right to reach full width.
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right

	return lipgloss.NewStyle().Width(width).Background(colorPanel).Render(line)
}

// renderTabStrip returns the one-row tab strip spanning width columns.
// The active view's tab is highlighted with the accent colour and a slightly
// lighter background to match the web dashboard's "active tab" treatment.
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
	inactiveStyle := lipgloss.NewStyle().
		Foreground(colorTextMuted)
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
//
// Layout:  ● cockpit.loop  agents N  projects M  $X  ·····  <claudeDir>  v<version>
func renderStatusBar(m Model, width int) string {
	agentCount := 0
	if m.manager != nil {
		agentCount = len(m.manager.List())
	}
	totalCost := 0.0
	for _, p := range m.projects {
		totalCost += p.LastCost
	}

	accent := lipgloss.NewStyle().Foreground(colorAccent)
	dim := lipgloss.NewStyle().Foreground(colorTextDim)
	muted := lipgloss.NewStyle().Foreground(colorTextMuted)
	costStyle := lipgloss.NewStyle().Foreground(colorCost)

	left := "  " +
		accent.Render("●") + "  " +
		muted.Render("cockpit.loop") +
		"   " + dim.Render(fmt.Sprintf("agents %d", agentCount)) +
		"   " + dim.Render(fmt.Sprintf("projects %d", len(m.projects))) +
		"   " + costStyle.Render(fmt.Sprintf("$%.2f", totalCost))

	right := muted.Render(truncate(m.claudeDir, 28)) +
		"   " + dim.Render("v"+build.Version) +
		"  "

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	dotCount := width - lw - rw - 4
	if dotCount < 3 {
		dotCount = 3
	}

	line := left + "  " + muted.Render(strings.Repeat("·", dotCount)) + "  " + right

	return lipgloss.NewStyle().Width(width).Background(colorPanel).Render(line)
}
