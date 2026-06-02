package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cockpit/internal/build"
	"cockpit/internal/store"
)

// issueEntry is a single line shown in the ISSUES section.
type issueEntry struct {
	icon    string
	kind    string
	message string
	project string
	timeStr string
}

const barWidth = 5

// costBar returns a 5-cell bar string scaled to maxCost.
// Example: cost=0.26, max=0.26 → "█████"; cost=0.04, max=0.26 → "█░░░░"
func costBar(cost, maxCost float64) string {
	if maxCost <= 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := int(cost/maxCost*float64(barWidth) + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

// truncate shortens s to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

// projectShortName returns the last component of a project path, or the raw
// path if it cannot be split.
func projectShortName(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." {
		return path
	}
	return base
}

// renderDashboard builds the full dashboard string for View().
func (m Model) renderDashboard() string {
	w := m.width
	if w < 60 {
		w = 60
	}

	var sb strings.Builder

	// ── Header ──────────────────────────────────────────────────────────────
	totalCost := 0.0
	for _, p := range m.projects {
		totalCost += p.LastCost
	}
	busyCount := 0
	for _, p := range m.processes {
		if p.Status == "busy" {
			busyCount++
		}
	}

	versionStr := fmt.Sprintf("v%s", build.Version)
	costStr := fmt.Sprintf("$%.2f today", totalCost)
	// Render styled parts individually; header uses raw fmt for layout.
	agentCount := 0
	if m.manager != nil {
		agentCount = len(m.manager.List())
	}
	agentStr := fmt.Sprintf("%d agents → press 2", agentCount)
	if busyCount > 0 {
		agentStr = styleBusy.Render(fmt.Sprintf("%d agents ●● → press 2", busyCount))
	}

	headerContent := fmt.Sprintf(" COCKPIT  %s | %s | %s | %s ",
		styleHeader.Render(versionStr),
		styleCost.Render(costStr),
		agentStr,
		styleDim.Render(fmt.Sprintf("%d projects", len(m.projects))),
	)
	// -2 for border width, -2 for inner padding
	sb.WriteString(styleBorder.Width(w - 4).Render(headerContent))
	sb.WriteString("\n")

	if m.loadErr != nil {
		sb.WriteString(styleError.Render(fmt.Sprintf("  ! load error: %v", m.loadErr)))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// ── Projects + Live processes (side by side) ────────────────────────────
	halfW := (w - 4) / 2

	// Projects column — build raw lines, then render each column cell via
	// lipgloss.NewStyle().Width() so ANSI-aware padding is used.
	var projectLines []string
	projectLines = append(projectLines, styleHeader.Render("  PROJECTS"))

	// Sort projects by cost descending so the bar scaling is meaningful.
	sorted := make([]store.ProjectSummary, len(m.projects))
	copy(sorted, m.projects)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LastCost > sorted[j].LastCost
	})

	maxCost := 0.0
	if len(sorted) > 0 {
		maxCost = sorted[0].LastCost
	}

	for _, p := range sorted {
		// Truncate and pad raw strings first, then render styles.
		name := truncate(projectShortName(p.Path), 14)
		bar := costBar(p.LastCost, maxCost)
		// Build raw layout using only plain-text strings for proper width math.
		rawCost := fmt.Sprintf("$%5.2f", p.LastCost)
		line := fmt.Sprintf("  %-14s  %s  %s",
			name,
			styleCost.Render(rawCost),
			bar,
		)
		projectLines = append(projectLines, line)
	}
	if len(sorted) == 0 {
		projectLines = append(projectLines, styleDim.Render("  (no projects)"))
	}

	// Live processes column
	var liveLines []string
	liveLines = append(liveLines, styleHeader.Render("  LIVE"))
	if len(m.processes) == 0 {
		liveLines = append(liveLines, styleDim.Render("  (no agents running)"))
	}
	for _, proc := range m.processes {
		indicator := styleIdle.Render("○")
		statusStr := styleDim.Render("idle")
		if proc.Status == "busy" {
			indicator = styleBusy.Render("●")
			statusStr = styleBusy.Render("busy")
		}
		label := proc.SessionID
		if label == "" {
			label = fmt.Sprintf("pid/%d", proc.PID)
		}
		// Use rune-safe truncate helper instead of byte slice.
		label = truncate(label, 12)
		liveLines = append(liveLines, fmt.Sprintf("  %s  %-12s  %s", indicator, label, statusStr))
	}

	// Render side-by-side using lipgloss column widths so ANSI-aware padding
	// is applied instead of byte-counting fmt padding.
	maxRows := len(projectLines)
	if len(liveLines) > maxRows {
		maxRows = len(liveLines)
	}
	leftStyle := lipgloss.NewStyle().Width(halfW)
	for i := 0; i < maxRows; i++ {
		left := ""
		if i < len(projectLines) {
			left = projectLines[i]
		}
		right := ""
		if i < len(liveLines) {
			right = liveLines[i]
		}
		// lipgloss.Width-aware padding ensures columns align correctly even
		// when left/right contain ANSI escape sequences.
		row := lipgloss.JoinHorizontal(lipgloss.Top, leftStyle.Render(left), "  "+right)
		sb.WriteString(row + "\n")
	}

	sb.WriteString("\n")

	// ── Recent Sessions ──────────────────────────────────────────────────────
	sb.WriteString(styleHeader.Render("  RECENT SESSIONS") + styleDim.Render(" (last 5)") + "\n")

	// Sort sessions newest-first and take top 5.
	sessions := make([]store.Session, len(m.sessions))
	copy(sessions, m.sessions)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	if len(sessions) > 5 {
		sessions = sessions[:5]
	}

	if len(sessions) == 0 {
		sb.WriteString(styleDim.Render("  (no sessions)") + "\n")
	}
	for _, s := range sessions {
		// Truncate all raw strings before any render calls.
		proj := truncate(projectShortName(s.ProjectPath), 12)
		ts := s.UpdatedAt.Format("2006-01-02 15:04")
		model := truncate(s.Model, 20)
		costRaw := fmt.Sprintf("$%.2f", s.CostUSD)
		// Build raw layout first, then apply style renders last.
		line := fmt.Sprintf("  %-12s  %s  %-20s  %s  %d edits",
			proj,
			styleDim.Render(ts),
			styleDim.Render(model),
			styleCost.Render(costRaw),
			s.EditCount,
		)
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")

	// ── Recent Commands ──────────────────────────────────────────────────────
	sb.WriteString(styleHeader.Render("  RECENT COMMANDS") + styleDim.Render(" (last 10)") + "\n")

	if len(m.history) == 0 {
		sb.WriteString(styleDim.Render("  (no history)") + "\n")
	}
	for _, h := range m.history {
		ts := h.Timestamp.Format("2006-01-02 15:04")
		// Truncate raw strings before composing the line.
		proj := truncate(h.Project, 12)
		display := truncate(h.Display, w-40)
		line := fmt.Sprintf("  %s  %-12s  %s",
			styleDim.Render(ts),
			proj,
			display,
		)
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")

	// ── Issues ───────────────────────────────────────────────────────────────
	issues := m.buildIssues(5)
	issueTitle := fmt.Sprintf("  ISSUES (%d)", len(issues))
	sb.WriteString(styleHeader.Render(issueTitle) + "\n")
	if len(issues) == 0 {
		sb.WriteString(styleBusy.Render("  ✓  No issues") + "\n")
	} else {
		for _, iss := range issues {
			icon := styleError.Render(iss.icon)
			kind := styleDim.Render(fmt.Sprintf("%-12s", iss.kind))
			msg := truncate(iss.message, w-50)
			proj := fmt.Sprintf("%-8s", truncate(iss.project, 8))
			ts := styleDim.Render(iss.timeStr)
			line := fmt.Sprintf("  %s  %s  %-30s  %s  %s",
				icon, kind, msg, proj, ts,
			)
			sb.WriteString(line + "\n")
		}
	}

	sb.WriteString("\n")

	// ── Footer ───────────────────────────────────────────────────────────────
	sep := strings.Repeat("─", w)
	sb.WriteString(styleDim.Render(sep) + "\n")
	sb.WriteString(styleDim.Render("  q quit   r refresh   n new agent   ? help   tab next view   1 dash   2 agents   3 tests   4 sessions") + "\n")

	return sb.String()
}

// buildIssues aggregates issues from recent sessions and the security log.
// Returns at most maxIssues entries.
func (m Model) buildIssues(maxIssues int) []issueEntry {
	var issues []issueEntry

	// 1. Sessions with tool errors.
	for _, s := range m.sessions {
		if s.ErrorCount > 0 {
			proj := projectShortName(s.ProjectPath)
			ts := s.UpdatedAt.Format("15:04:05")
			msg := fmt.Sprintf("%d tool error(s) in %s session %s",
				s.ErrorCount, proj, s.UpdatedAt.Format("2006-01-02 15:04"))
			issues = append(issues, issueEntry{
				icon:    "✕",
				kind:    "tool_error",
				message: msg,
				project: proj,
				timeStr: ts,
			})
		}
		if len(issues) >= maxIssues {
			return issues
		}
	}

	// 2. Security log entries.
	secEntries, err := store.ReadSecLog(m.claudeDir, maxIssues-len(issues))
	if err == nil {
		for _, e := range secEntries {
			ts := "-"
			if !e.Timestamp.IsZero() {
				ts = e.Timestamp.Format("15:04:05")
			}
			msg := truncate(e.Raw, 60)
			issues = append(issues, issueEntry{
				icon:    "⚠",
				kind:    "sec_log",
				message: msg,
				project: "-",
				timeStr: ts,
			})
			if len(issues) >= maxIssues {
				break
			}
		}
	}

	return issues
}
