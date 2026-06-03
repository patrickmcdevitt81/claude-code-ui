package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHelp returns the keybindings help overlay as a string.
// termWidth is the current terminal width; the box is clamped to fit.
func renderHelp(termWidth int) string {
	boxWidth := 72 // nominal width
	if termWidth-4 < boxWidth {
		boxWidth = termWidth - 4
	}
	if boxWidth < 40 {
		boxWidth = 40 // absolute minimum
	}

	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 2).
		Width(boxWidth).
		Background(colorPanel).
		BorderForeground(colorBorder)

	accent := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colorTextMuted)

	var sb strings.Builder

	section := func(title string) {
		sb.WriteString("\n")
		sb.WriteString(sectionLabel(title) + "\n")
	}

	row := func(key, desc string) {
		// Use lipgloss width so ANSI escapes don't confuse column padding.
		keyCol := lipgloss.NewStyle().Width(28).Render(accent.Render(key))
		sb.WriteString(fmt.Sprintf("  %s  %s\n", keyCol, dim.Render(desc)))
	}

	sb.WriteString(accent.Render("KEYBINDINGS") + "\n")

	section("GLOBAL")
	row("q / ctrl+c", "quit")
	row("tab", "next view")
	row("1 dash  2 agents  3 tests  4 sessions", "jump to view")

	section("DASHBOARD")
	row("r", "refresh data")
	row("n", "launch new agent (and focus)")

	section("AGENTS")
	row("↑/↓  j/k", "navigate")
	row("f / enter", "focus selected agent (full PTY passthrough)")
	row("K", "kill selected agent")
	row("L", "launch new agent in custom directory")

	section("TESTS")
	row("w", "toggle watch mode")
	row("r", "run tests once")
	row("d", "change test directory")

	section("SESSIONS")
	row("↑/↓", "navigate")
	row("/", "search sessions")
	row("enter", "resume session (spawns new PTY agent)")

	section("EVERYWHERE")
	row("?", "toggle this help overlay")

	return helpStyle.Render(sb.String())
}
