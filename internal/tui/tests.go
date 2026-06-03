package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cockpit/internal/runner"
)

// testPollMsg is sent every second to pick up new runner results.
type testPollMsg struct{}

// doTestPoll returns a Bubble Tea command that fires testPollMsg after one second.
func doTestPoll() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return testPollMsg{}
	})
}

// stylePass / styleFail reuse theme semantic colours.
var (
	stylePass = lipgloss.NewStyle().Foreground(colorGreen)
	styleFail = lipgloss.NewStyle().Foreground(colorRed)
)

// renderTests builds the tests-view body string.
// The header chrome and keybinding footer are provided by model.View().
func renderTests(m Model) string {
	w := m.width
	if w < 72 {
		w = 72
	}

	var sb strings.Builder

	// ── Summary line ─────────────────────────────────────────────────────────
	var watchIndicator string
	if m.runner != nil && m.runner.IsWatching() {
		watchIndicator = styleBusy.Render(frame(spinnerFrames, m.animFrame) + " watching")
	} else {
		watchIndicator = styleDim.Render("○ idle")
	}

	var lastRunStr string
	if m.runnerResult != nil {
		ago := time.Since(m.runnerResult.StartedAt)
		lastRunStr = fmt.Sprintf("last run: %s ago", formatAge(ago))
	} else {
		lastRunStr = "no run yet"
	}

	testCmd := "─"
	if m.runner != nil {
		if res := m.runner.LastResult(); res != nil {
			testCmd = res.Command
		}
	}

	summary := fmt.Sprintf("  %s   %s   %s   %s",
		sectionLabel(truncate(m.testCWD, 40)),
		styleDim.Render(truncate(testCmd, 24)),
		watchIndicator,
		styleDim.Render(lastRunStr),
	)
	sb.WriteString(summary + "\n\n")

	// ── Directory input (shown when d is pressed) ────────────────────────────
	if m.testChangingDir {
		sb.WriteString(fmt.Sprintf("  Change test directory: [%s]  enter to confirm, esc to cancel\n\n",
			m.testDirInput.View()))
	}

	// ── Last run ─────────────────────────────────────────────────────────────
	if m.runnerResult != nil {
		res := m.runnerResult
		ts := res.StartedAt.Format("2006-01-02 15:04:05")
		dur := fmt.Sprintf("%.1fs", res.Duration.Seconds())

		headerLine := fmt.Sprintf("  LAST RUN — %s   took %s", ts, dur)
		sb.WriteString(styleHeader.Render(headerLine) + "\n\n")

		// Output lines.
		for _, line := range res.Output {
			rendered := formatTestLine(line)
			sb.WriteString("  " + rendered + "\n")
		}
		if len(res.Output) == 0 {
			sb.WriteString(styleDim.Render("  (no output)") + "\n")
		}
	} else {
		sb.WriteString(styleDim.Render("  No test run yet. Press r to run, w to start watching.") + "\n")
	}

	sb.WriteString("\n")

	// ── History ──────────────────────────────────────────────────────────────
	if len(m.runHistory) > 0 {
		sb.WriteString(sectionLabel("  HISTORY") +
			styleDim.Render(fmt.Sprintf(" (last %d runs)", len(m.runHistory))) + "\n")

		for _, res := range m.runHistory {
			ts := res.StartedAt.Format("15:04:05")
			dur := fmt.Sprintf("%.1fs", res.Duration.Seconds())
			icon := stylePass.Render("✓")
			if res.Failed {
				icon = styleFail.Render("✗")
			}
			line := fmt.Sprintf("  %s  %s  %s   %s",
				styleDim.Render(ts),
				icon,
				styleDim.Render(dur),
				res.Summary,
			)
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	// Keep w referenced to avoid "declared and not used" if the body shrinks.
	_ = w

	sb.WriteString("\n")

	return sb.String()
}

// formatTestLine applies green/red styling to go test output lines.
func formatTestLine(line string) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "--- PASS:"), strings.HasPrefix(trimmed, "ok "):
		return stylePass.Render(line)
	case strings.HasPrefix(trimmed, "--- FAIL:"),
		trimmed == "FAIL",
		strings.HasPrefix(trimmed, "FAIL\t"),
		strings.HasPrefix(trimmed, "FAIL "):
		return styleFail.Render(line)
	default:
		return line
	}
}

// formatAge converts a duration to a short human-readable string.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// resultChanged reports whether newResult is different from oldResult (by
// comparing StartedAt timestamps).
func resultChanged(old, newR *runner.Result) bool {
	if old == nil && newR == nil {
		return false
	}
	if old == nil || newR == nil {
		return true
	}
	return !old.StartedAt.Equal(newR.StartedAt)
}
