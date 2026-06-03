package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

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

// priorityItem is a ranked next-action recommendation for the developer.
type priorityItem struct {
	urgency int    // 0=critical  1=medium  2=suggestion
	icon    string
	action  string
	detail  string
}

const barWidth = 5

// costBar returns a barWidth-cell gradient bar scaled to maxCost.
func costBar(cost, maxCost float64) string {
	if maxCost <= 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := int(cost/maxCost*float64(barWidth) + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	var b strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < filled {
			switch {
			case filled == barWidth:
				b.WriteString("█")
			case i < filled-1:
				b.WriteString("▓")
			default:
				b.WriteString("▒")
			}
		} else {
			b.WriteString("░")
		}
	}
	return b.String()
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

// projectShortName returns the last path component.
func projectShortName(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." {
		return path
	}
	return base
}

// projectDailyCosts returns the last `days` daily cost totals for projectPath,
// ordered oldest→newest (index 0 = `days` ago, index days-1 = today).
func projectDailyCosts(projectPath string, sessions []store.Session, days int) []float64 {
	now := time.Now()
	daily := make([]float64, days)
	for _, s := range sessions {
		if s.ProjectPath != projectPath {
			continue
		}
		dayIdx := int(now.Sub(s.UpdatedAt).Hours() / 24)
		if dayIdx >= 0 && dayIdx < days {
			daily[days-1-dayIdx] += s.CostUSD
		}
	}
	return daily
}

// renderSparkline returns a width-char sparkline for values (oldest→newest).
func renderSparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return strings.Repeat("·", width)
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	maxVal := 0.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	var sb strings.Builder
	for _, v := range values {
		frac := 0.0
		if maxVal > 0 {
			frac = v / maxVal
		}
		sb.WriteString(sparklineChar(frac))
	}
	return sb.String()
}

// claudeLogo returns the 5 lines of the animated Claude "C" ASCII art.
// The spark character inside the C opening cycles with animFrame.
func claudeLogo(animFrame int) []string {
	sp := frame(sparkFrames, animFrame)
	return []string{
		styleOrange.Render(" ██████╗"),
		styleOrange.Render("██╔════╝"),
		styleOrange.Render("██║") + " " + styleCost.Render(sp),
		styleOrange.Render("╚██████╗"),
		styleOrange.Render(" ╚═════╝"),
	}
}

// buildPriorityItems returns ranked next-action items based on current state.
func (m Model) buildPriorityItems() []priorityItem {
	var items []priorityItem

	// 1. Failing tests — critical.
	if m.runnerResult != nil && m.runnerResult.Failed {
		items = append(items, priorityItem{
			urgency: 0, icon: "✗",
			action: "Fix failing tests",
			detail: m.runnerResult.Summary,
		})
	}

	// 2. Sessions with tool errors — critical.
	for _, s := range m.sessions {
		if s.ErrorCount > 0 {
			items = append(items, priorityItem{
				urgency: 0, icon: "!",
				action: "Review errors → " + projectShortName(s.ProjectPath),
				detail: fmt.Sprintf("%d tool error(s)  press 4", s.ErrorCount),
			})
			break
		}
	}

	// 3. No agents running but recent work exists — medium.
	agentCount := 0
	if m.manager != nil {
		agentCount = len(m.manager.List())
	}
	if agentCount == 0 && len(m.sessions) > 0 {
		s := m.sessions[0]
		items = append(items, priorityItem{
			urgency: 1, icon: "→",
			action: "Resume: " + truncate(projectShortName(s.ProjectPath), 18),
			detail: s.UpdatedAt.Format("Mon 15:04") + "  press 4→enter",
		})
	}

	// 4. Tests not run while agents are active — medium.
	if m.runnerResult == nil && agentCount > 0 {
		items = append(items, priorityItem{
			urgency: 1, icon: "⚑",
			action: "Run tests to verify agent work",
			detail: "press 3 → r",
		})
	}

	// 5. High-cost project worth reviewing — suggestion.
	for _, p := range m.projects {
		if p.LastCost > 0.5 {
			items = append(items, priorityItem{
				urgency: 2, icon: "$",
				action: "Review spend: " + truncate(projectShortName(p.Path), 16),
				detail: fmt.Sprintf("$%.2f  press 4", p.LastCost),
			})
			break
		}
	}

	if len(items) == 0 {
		items = append(items, priorityItem{
			urgency: 2, icon: "✦",
			action: "All clear — start a new agent",
			detail: "press n",
		})
	}

	return items
}

// renderDashboard builds the dashboard body string for View().
// The branded header, tab strip, and footer chrome are composed in model.View().
func (m Model) renderDashboard() string {
	w := m.width
	if w < 60 {
		w = 60
	}

	var sb strings.Builder

	if m.loadErr != nil {
		sb.WriteString(styleError.Render(fmt.Sprintf("  ! load error: %v", m.loadErr)))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// ── Claude "C" logo + ⚡ FOCUS NEXT panel ────────────────────────────────
	logo := claudeLogo(m.animFrame)
	priority := m.buildPriorityItems()

	logoW := 12
	priorityW := w - logoW - 6
	if priorityW < 30 {
		priorityW = 30
	}

	urgencyBullet := [3]string{"►", "►", "·"}
	applyUrgency := func(urgency int, s string) string {
		switch urgency {
		case 0:
			return styleError.Render(s)
		case 1:
			return styleCost.Render(s)
		default:
			return styleDim.Render(s)
		}
	}

	pLines := []string{styleOrange.Render("⚡ FOCUS NEXT")}
	for i, p := range priority {
		if i >= 4 {
			break
		}
		ui := applyUrgency(p.urgency, urgencyBullet[p.urgency])
		action := applyUrgency(p.urgency, truncate(p.action, 26))
		detail := truncate(p.detail, priorityW-32)
		pLines = append(pLines,
			fmt.Sprintf("  %s %-26s  %s", ui, action, styleDim.Render(detail)))
	}
	for len(logo) < 5 {
		logo = append(logo, "")
	}
	for len(pLines) < 5 {
		pLines = append(pLines, "")
	}
	logoCol := lipgloss.NewStyle().Width(logoW)
	for i := 0; i < 5; i++ {
		sb.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Top, logoCol.Render(logo[i]), "  "+pLines[i]) + "\n",
		)
	}

	sb.WriteString("\n")

	// ── Projects + Live processes (side by side) ─────────────────────────────
	halfW := (w - 4) / 2

	sorted := make([]store.ProjectSummary, len(m.projects))
	copy(sorted, m.projects)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LastCost > sorted[j].LastCost
	})
	maxCost := 0.0
	if len(sorted) > 0 {
		maxCost = sorted[0].LastCost
	}

	var projectLines []string
	projectLines = append(projectLines,
		sectionLabel("  PROJECTS")+styleDim.Render("  cost   bar   7d ▸"),
	)
	for _, p := range sorted {
		name := truncate(projectShortName(p.Path), 12)
		bar := styleAmber.Render(costBar(p.LastCost, maxCost))
		rawCost := fmt.Sprintf("$%5.2f", p.LastCost)
		daily := projectDailyCosts(p.Path, m.allSessions, 7)
		spark := styleCyan.Render(renderSparkline(daily, 7))
		projectLines = append(projectLines,
			fmt.Sprintf("  %-12s  %s  %s  %s",
				name, styleCost.Render(rawCost), bar, spark))
	}
	if len(sorted) == 0 {
		projectLines = append(projectLines, styleDim.Render("  (no projects)"))
	}

	var liveLines []string
	liveLines = append(liveLines, sectionLabel("  LIVE"))
	if len(m.processes) == 0 {
		liveLines = append(liveLines, styleDim.Render("  (no agents running)"))
	}
	for _, proc := range m.processes {
		var indicator, statusStr string
		if proc.Status == "busy" {
			indicator = styleBusy.Render(frame(spinnerFrames, m.animFrame))
			statusStr = styleBusy.Render("busy")
		} else {
			indicator = styleIdle.Render("○")
			statusStr = styleDim.Render("idle")
		}
		label := proc.SessionID
		if label == "" {
			label = fmt.Sprintf("pid/%d", proc.PID)
		}
		label = truncate(label, 12)
		liveLines = append(liveLines, fmt.Sprintf("  %s  %-12s  %s", indicator, label, statusStr))
	}

	maxRows := len(projectLines)
	if len(liveLines) > maxRows {
		maxRows = len(liveLines)
	}
	leftStyle := lipgloss.NewStyle().Width(halfW)
	for i := 0; i < maxRows; i++ {
		left, right := "", ""
		if i < len(projectLines) {
			left = projectLines[i]
		}
		if i < len(liveLines) {
			right = liveLines[i]
		}
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftStyle.Render(left), "  "+right) + "\n")
	}

	sb.WriteString("\n")

	// ── Recent Sessions ───────────────────────────────────────────────────────
	sb.WriteString(sectionLabel("  RECENT SESSIONS") + styleDim.Render(" (last 5)") + "\n")
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
		proj := truncate(projectShortName(s.ProjectPath), 12)
		ts := s.UpdatedAt.Format("2006-01-02 15:04")
		modelStr := truncate(s.Model, 20)
		costRaw := fmt.Sprintf("$%.2f", s.CostUSD)
		recency := ""
		if time.Since(s.UpdatedAt) < 10*time.Minute {
			recency = " " + styleBusy.Render("●")
		}
		sb.WriteString(fmt.Sprintf("  %-12s  %s  %-20s  %s  %d edits%s\n",
			proj, styleDim.Render(ts), styleDim.Render(modelStr),
			styleCost.Render(costRaw), s.EditCount, recency))
	}

	sb.WriteString("\n")

	// ── Recent Commands ───────────────────────────────────────────────────────
	sb.WriteString(sectionLabel("  RECENT COMMANDS") + styleDim.Render(" (last 10)") + "\n")
	if len(m.history) == 0 {
		sb.WriteString(styleDim.Render("  (no history)") + "\n")
	}
	for _, h := range m.history {
		ts := h.Timestamp.Format("2006-01-02 15:04")
		proj := truncate(h.Project, 12)
		display := truncate(h.Display, w-40)
		sb.WriteString(fmt.Sprintf("  %s  %-12s  %s\n",
			styleDim.Render(ts), proj, display))
	}

	sb.WriteString("\n")

	// ── Issues ────────────────────────────────────────────────────────────────
	issues := m.buildIssues(5)
	sb.WriteString(sectionLabel(fmt.Sprintf("  ISSUES (%d)", len(issues))) + "\n")
	if len(issues) == 0 {
		sb.WriteString(styleMint.Render("  ✓  No issues — looking good") + "\n")
	} else {
		for _, iss := range issues {
			sb.WriteString(fmt.Sprintf("  %s  %s  %-30s  %-8s  %s\n",
				styleError.Render(iss.icon),
				styleDim.Render(fmt.Sprintf("%-12s", iss.kind)),
				truncate(iss.message, w-50),
				truncate(iss.project, 8),
				styleDim.Render(iss.timeStr)))
		}
	}

	sb.WriteString("\n")

	return sb.String()
}

// buildIssues aggregates issues from recent sessions and the security log.
func (m Model) buildIssues(maxIssues int) []issueEntry {
	var issues []issueEntry
	for _, s := range m.sessions {
		if s.ErrorCount > 0 {
			proj := projectShortName(s.ProjectPath)
			issues = append(issues, issueEntry{
				icon:    "✕",
				kind:    "tool_error",
				message: fmt.Sprintf("%d tool error(s) in %s session %s", s.ErrorCount, proj, s.UpdatedAt.Format("2006-01-02 15:04")),
				project: proj,
				timeStr: s.UpdatedAt.Format("15:04:05"),
			})
		}
		if len(issues) >= maxIssues {
			return issues
		}
	}
	secEntries, err := store.ReadSecLog(m.claudeDir, maxIssues-len(issues))
	if err == nil {
		for _, e := range secEntries {
			ts := "-"
			if !e.Timestamp.IsZero() {
				ts = e.Timestamp.Format("15:04:05")
			}
			issues = append(issues, issueEntry{
				icon: "⚠", kind: "sec_log",
				message: truncate(e.Raw, 60), project: "-", timeStr: ts,
			})
			if len(issues) >= maxIssues {
				break
			}
		}
	}
	return issues
}
