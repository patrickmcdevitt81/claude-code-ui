package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"cockpit/internal/store"
)

// renderSessions builds the sessions-view body string.
// The header chrome and keybinding footer are provided by model.View().
func renderSessions(m Model) string {
	w := m.width
	if w < 72 {
		w = 72
	}

	var sb strings.Builder

	// ── Summary line ─────────────────────────────────────────────────────────
	totalCost := 0.0
	costByProject := map[string]float64{}
	for _, s := range m.allSessions {
		name := projectShortName(s.ProjectPath)
		totalCost += s.CostUSD
		costByProject[name] += s.CostUSD
	}

	// Build cost summary string: "arc: $7.44   cockpit: $0.04"
	type projCost struct {
		name string
		cost float64
	}
	var projCosts []projCost
	for name, cost := range costByProject {
		projCosts = append(projCosts, projCost{name, cost})
	}
	sort.Slice(projCosts, func(i, j int) bool {
		return projCosts[i].cost > projCosts[j].cost
	})
	var costParts []string
	for _, pc := range projCosts {
		costParts = append(costParts, fmt.Sprintf("%s: $%.2f", pc.name, pc.cost))
	}
	costSummary := strings.Join(costParts, "   ")
	if costSummary == "" {
		costSummary = "$0.00"
	}

	// Build search display.
	var searchDisplay string
	if m.sessionSearching {
		searchDisplay = fmt.Sprintf("search: [%s]", m.sessionInput.View())
	} else if m.sessionSearch != "" {
		searchDisplay = fmt.Sprintf("search: %s (esc clear)", m.sessionSearch)
	} else {
		searchDisplay = "/ to search"
	}

	summary := fmt.Sprintf("  %s   %s   %s",
		sectionLabel(fmt.Sprintf("%d sessions  $%.2f total", len(m.allSessions), totalCost)),
		styleCost.Render(truncate(costSummary, 40)),
		styleDim.Render(searchDisplay),
	)
	sb.WriteString(summary + "\n\n")

	// ── Column header ─────────────────────────────────────────────────────────
	colHeader := fmt.Sprintf("  %-12s  %-16s  %-22s  %-7s  %-5s  %s",
		"PROJECT", "DATE", "MODEL", "COST", "EDITS", "MSGS",
	)
	sb.WriteString(sectionLabel(colHeader) + "\n")
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")

	// ── Session rows ──────────────────────────────────────────────────────────
	sessions := m.filteredSessions
	maxVisible := 10
	if len(sessions) < maxVisible {
		maxVisible = len(sessions)
	}

	// Show a window around the selected index.
	startIdx := 0
	if m.selectedSessionIdx >= maxVisible {
		startIdx = m.selectedSessionIdx - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(sessions) {
		endIdx = len(sessions)
		startIdx = endIdx - maxVisible
		if startIdx < 0 {
			startIdx = 0
		}
	}

	for i := startIdx; i < endIdx; i++ {
		s := sessions[i]
		proj := truncate(projectShortName(s.ProjectPath), 12)
		ts := s.UpdatedAt.Format("Jan 02 15:04    ")
		modelStr := truncate(s.Model, 22)
		costStr := fmt.Sprintf("$%.2f", s.CostUSD)

		rawLine := fmt.Sprintf("  %-12s  %-16s  %-22s  %-7s  %-5d  %d",
			proj, ts, modelStr, costStr, s.EditCount, s.MessageCount,
		)

		if i == m.selectedSessionIdx {
			sb.WriteString(styleSelected.Render(rawLine))
		} else {
			sb.WriteString(styleDim.Render(rawLine))
		}
		sb.WriteString("\n")
	}

	if len(sessions) == 0 {
		msg := "  (no sessions)"
		if m.sessionSearch != "" {
			msg = fmt.Sprintf("  (no sessions match %q)", m.sessionSearch)
		}
		sb.WriteString(styleDim.Render(msg) + "\n")
	}

	sb.WriteString("\n")

	// ── Detail pane for selected session ──────────────────────────────────────
	sb.WriteString(styleDim.Render(strings.Repeat("─", w)) + "\n")

	if m.selectedSessionIdx < len(sessions) {
		s := sessions[m.selectedSessionIdx]

		proj := projectShortName(s.ProjectPath)
		ts := s.UpdatedAt.Format("Jan 02 15:04    ")
		costStr := fmt.Sprintf("$%.2f", s.CostUSD)
		errStr := ""
		if s.ErrorCount > 0 {
			errStr = styleError.Render(fmt.Sprintf("   %d errors", s.ErrorCount))
		}

		detail := fmt.Sprintf("  %s   %s   %s   %s   %d edits   %d msgs%s",
			styleHeader.Render(proj),
			styleDim.Render(ts),
			styleDim.Render(s.Model),
			styleCost.Render(costStr),
			s.EditCount,
			s.MessageCount,
			errStr,
		)
		sb.WriteString(detail + "\n")

		if s.GitBranch != "" {
			sb.WriteString(styleDim.Render(fmt.Sprintf("  GitBranch: %s", s.GitBranch)) + "\n")
		}

		sb.WriteString("\n")

		// Recent edits from the pre-loaded list (capped at 5 in model).
		sb.WriteString(sectionLabel("  RECENT EDITS:") + "\n")
		if len(m.sessionEdits) == 0 {
			sb.WriteString(styleDim.Render("  (none)") + "\n")
		}
		for _, e := range m.sessionEdits {
			ets := e.Timestamp.Format("15:04:05")
			fp := truncate(e.FilePath, w-30)
			line := fmt.Sprintf("  %s  %-6s  %s",
				styleDim.Render(ets),
				e.ToolName,
				fp,
			)
			sb.WriteString(line + "\n")
		}

		// Active tasks for this session.
		if tasks, ok := m.tasks[s.SessionID]; ok {
			active := 0
			for _, t := range tasks {
				if t.Status == "in_progress" || t.Status == "pending" {
					active++
				}
			}
			if active > 0 {
				sb.WriteString("\n")
				sb.WriteString(sectionLabel("  TASKS:") + "\n")
				for _, t := range tasks {
					if t.Status == "completed" || t.Status == "deleted" {
						continue
					}
					icon := styleDim.Render("◦")
					if t.Status == "in_progress" {
						icon = styleBusy.Render("●")
					}
					sb.WriteString(fmt.Sprintf("  %s  %-10s  %s\n",
						icon, styleDim.Render(t.Status), truncate(t.Subject, w-28)))
				}
			}
		}

		// Error count summary.
		if s.ErrorCount > 0 {
			sb.WriteString("\n")
			sb.WriteString(styleError.Render(fmt.Sprintf("  ERRORS: %d tool errors in this session", s.ErrorCount)) + "\n")
		}
	} else {
		sb.WriteString(styleDim.Render("  (no session selected)") + "\n")
	}

	sb.WriteString("\n")

	// ── Error banner (shown when resume or other operations fail) ─────────────
	if m.loadErr != nil {
		sb.WriteString(styleError.Render(fmt.Sprintf("  ERROR: %s", m.loadErr.Error())) + "\n\n")
	}

	// ── Footer hint ────────────────────────────────────────────────────────────
	footerItems := []string{"↑/↓ navigate", "enter resume", "/ search", "esc clear"}
	var footerParts []string
	for _, item := range footerItems {
		footerParts = append(footerParts, styleDim.Render(item))
	}
	sb.WriteString("  " + strings.Join(footerParts, "  ·  ") + "\n")

	return sb.String()
}

// filterSessions returns sessions matching the search query (case-insensitive).
// Matches on project path, model name, and date string.
func filterSessions(sessions []store.Session, query string) []store.Session {
	if query == "" {
		return sessions
	}
	q := strings.ToLower(query)
	var out []store.Session
	for _, s := range sessions {
		proj := strings.ToLower(filepath.Base(s.ProjectPath))
		model := strings.ToLower(s.Model)
		date := strings.ToLower(s.UpdatedAt.Format("2006-01-02"))
		if strings.Contains(proj, q) || strings.Contains(model, q) || strings.Contains(date, q) {
			out = append(out, s)
		}
	}
	return out
}

// sortSessionsNewestFirst sorts sessions descending by UpdatedAt in-place.
func sortSessionsNewestFirst(sessions []store.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
}
