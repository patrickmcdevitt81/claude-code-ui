package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"cockpit/internal/agent"
)

// agentStatus classifies a managed agent.
type agentStatus int

const (
	statusBusy agentStatus = iota
	statusIdle
	statusDead
)

// classifyAgent determines whether agent a is BUSY, IDLE, or DEAD.
//
//   - DEAD : the done channel is already closed (process exited).
//   - BUSY : the agent's session ID appears in m.processes with status=="busy".
//   - IDLE : alive but not currently processing a prompt.
func classifyAgent(a *agent.Agent, m Model) agentStatus {
	select {
	case <-a.Wait():
		return statusDead
	default:
	}

	sid := a.GetSessionID()
	if sid != "" {
		for _, p := range m.processes {
			if p.SessionID == sid {
				if p.Status == "busy" {
					return statusBusy
				}
				return statusIdle
			}
		}
	}
	return statusIdle
}

// lookupSessionByID returns session cost/token info for a given session ID, or nil.
// Searches m.sessions (fast path, recent 10) then m.allSessions (full 200).
func lookupSessionByID(sid string, m Model) *sessionInfo {
	if sid == "" {
		return nil
	}
	for i := range m.sessions {
		if m.sessions[i].SessionID == sid {
			s := &m.sessions[i]
			return &sessionInfo{
				model:     s.Model,
				costUSD:   s.CostUSD,
				inTokens:  s.TotalInputTokens,
				outTokens: s.TotalOutputTokens,
			}
		}
	}
	for i := range m.allSessions {
		if m.allSessions[i].SessionID == sid {
			s := &m.allSessions[i]
			return &sessionInfo{
				model:     s.Model,
				costUSD:   s.CostUSD,
				inTokens:  s.TotalInputTokens,
				outTokens: s.TotalOutputTokens,
			}
		}
	}
	return nil
}

// lookupSession returns the store.Session info for an agent, or nil.
func lookupSession(a *agent.Agent, m Model) *sessionInfo {
	return lookupSessionByID(a.GetSessionID(), m)
}

type sessionInfo struct {
	model     string
	costUSD   float64
	inTokens  int64
	outTokens int64
}

// formatTokens converts a raw token count to a compact string like "12k".
func formatTokens(n int64) string {
	if n == 0 {
		return "0"
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// sortedAgents returns the agent list in a stable order (by ID).
func sortedAgents(agents []*agent.Agent) []*agent.Agent {
	out := make([]*agent.Agent, len(agents))
	copy(out, agents)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Styles used only in the agents view.
var (
	styleDead = lipgloss.NewStyle().Foreground(colorRed)
	styleSelected = lipgloss.NewStyle().
			Background(colorBorderActive).
			Foreground(colorAccent)
	styleNewRow = lipgloss.NewStyle().Foreground(colorGreen)
)

// renderAgents builds the agents-view body string.
// Section 1: LIVE SESSIONS — all externally-running claude processes from m.processes.
// Section 2: PTY AGENTS — cockpit-launched agents (secondary).
func renderAgents(m Model) string {
	w := m.width
	if w < 72 {
		w = 72
	}

	var ptyAgents []*agent.Agent
	if m.manager != nil {
		ptyAgents = sortedAgents(m.manager.List())
	}

	// ── Tally (live processes are the primary count) ───────────────────────────
	busyCount, idleCount := 0, 0
	totalCost := 0.0
	for _, p := range m.processes {
		if p.Status == "busy" {
			busyCount++
		} else {
			idleCount++
		}
		if si := lookupSessionByID(p.SessionID, m); si != nil {
			totalCost += si.costUSD
		}
	}

	var sb strings.Builder

	// ── Summary bar ────────────────────────────────────────────────────────────
	summaryParts := fmt.Sprintf("  %s  %s  %s",
		styleBusy.Render(fmt.Sprintf("%d running", busyCount)),
		styleDim.Render(fmt.Sprintf("%d idle", idleCount)),
		styleCost.Render(fmt.Sprintf("$%.2f session cost", totalCost)),
	)
	if m.focusedID != "" {
		summaryParts += "  " + styleAmber.Render("● attached")
	}
	sb.WriteString(summaryParts + "\n\n")

	// ── LIVE SESSIONS section ──────────────────────────────────────────────────
	sb.WriteString(sectionLabel("  LIVE SESSIONS") + "\n")
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")

	if len(m.processes) == 0 {
		sb.WriteString(styleDim.Render("  (no active claude sessions)") + "\n")
	}
	for i, proc := range m.processes {
		var indicator string
		if proc.Status == "busy" {
			indicator = styleBusy.Render(frame(spinnerFrames, m.animFrame))
		} else {
			indicator = styleIdle.Render("○")
		}
		cwdBase := filepath.Base(proc.CWD)
		if cwdBase == "" || cwdBase == "." {
			cwdBase = proc.CWD
		}
		uptime := liveUptime(proc.StartedAt)
		costStr := "─"
		if si := lookupSessionByID(proc.SessionID, m); si != nil {
			costStr = fmt.Sprintf("$%.2f", si.costUSD)
		}
		activeTaskCount := 0
		for _, t := range m.tasks[proc.SessionID] {
			if t.Status == "in_progress" || t.Status == "pending" {
				activeTaskCount++
			}
		}
		taskStr := ""
		if activeTaskCount > 0 {
			taskStr = "  " + styleBusy.Render(fmt.Sprintf("%d task(s)", activeTaskCount))
		}

		rawLine := fmt.Sprintf("  %s  %-16s  %-5s  %-7s  %s%s",
			indicator,
			truncate(cwdBase, 16),
			uptime,
			styleCost.Render(costStr),
			styleDim.Render(proc.Status),
			taskStr,
		)
		if i == m.selectedAgentIdx {
			sb.WriteString(styleSelected.Render(rawLine) + "\n")
		} else if proc.Status == "busy" {
			sb.WriteString(styleBusy.Render(rawLine) + "\n")
		} else {
			sb.WriteString(styleDim.Render(rawLine) + "\n")
		}
	}

	// ── PTY AGENTS section (secondary) ────────────────────────────────────────
	sb.WriteString("\n")
	sb.WriteString(sectionLabel("  PTY AGENTS") + styleDim.Render("  (cockpit-managed)") + "\n")
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")

	const (
		cID  = 6
		cSt  = 5
		cUp  = 5
		cCWD = 28
		cCst = 7
		cSes = 16
	)

	for i, a := range ptyAgents {
		status := classifyAgent(a, m)
		si := lookupSession(a, m)

		idShort := a.ID
		if len(idShort) > cID {
			idShort = idShort[:cID]
		}
		var stateWord string
		switch status {
		case statusBusy:
			stateWord = frame(spinnerFrames, m.animFrame) + "RUN"
		case statusIdle:
			stateWord = "○IDL"
		default:
			stateWord = "✕END"
		}
		costStr := "─"
		sessStr := "─"
		if si != nil {
			costStr = fmt.Sprintf("$%.2f", si.costUSD)
			sessStr = truncate(si.model, cSes)
		}
		focusMark := " "
		if a.ID == m.focusedID {
			focusMark = "●"
		}
		rawLine := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s %s",
			cID, idShort,
			cSt, stateWord,
			cUp, a.UptimeStr(),
			cCWD, truncate(a.CWD, cCWD),
			cCst, costStr,
			cSes, sessStr,
			focusMark,
		)
		globalIdx := len(m.processes) + i
		var renderedLine string
		if globalIdx == m.selectedAgentIdx {
			renderedLine = styleSelected.Render(rawLine)
		} else {
			switch status {
			case statusBusy:
				renderedLine = styleBusy.Render(rawLine)
			case statusDead:
				renderedLine = styleDead.Render(rawLine)
			default:
				renderedLine = styleDim.Render(rawLine)
			}
		}
		sb.WriteString(renderedLine + "\n")
	}

	// [new] row
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")
	newRowText := "  [new]   + LAUNCH   press L to launch in a custom directory"
	newIdx := len(m.processes) + len(ptyAgents)
	if newIdx == m.selectedAgentIdx {
		sb.WriteString(styleSelected.Render(newRowText))
	} else {
		sb.WriteString(styleNewRow.Render(newRowText))
	}
	sb.WriteString("\n")

	// ── Detail pane ────────────────────────────────────────────────────────────
	sb.WriteString("\n")
	if m.selectedAgentIdx < len(m.processes) {
		proc := m.processes[m.selectedAgentIdx]
		si := lookupSessionByID(proc.SessionID, m)
		modelName, inTok, outTok := "─", "─", "─"
		if si != nil {
			modelName = si.model
			inTok = formatTokens(si.inTokens)
			outTok = formatTokens(si.outTokens)
		}
		detail := fmt.Sprintf("  %s %s   %s   in=%s  out=%s",
			sectionLabel("SESSION:"),
			styleDim.Render(truncate(proc.CWD, 44)),
			styleDim.Render(modelName),
			styleCyan.Render(inTok),
			styleCyan.Render(outTok),
		)
		sb.WriteString(detail + "\n")
	} else {
		ptyIdx := m.selectedAgentIdx - len(m.processes)
		if ptyIdx < len(ptyAgents) {
			a := ptyAgents[ptyIdx]
			si := lookupSession(a, m)
			modelName, inTok, outTok := "─", "─", "─"
			if si != nil {
				modelName = si.model
				inTok = formatTokens(si.inTokens)
				outTok = formatTokens(si.outTokens)
			}
			detail := fmt.Sprintf("  %s %s   %s   %s   in=%s  out=%s",
				sectionLabel("AGENT:"),
				styleAmber.Render(a.ID[:min6(len(a.ID))]),
				styleDim.Render(truncate(a.CWD, 44)),
				styleDim.Render(modelName),
				styleCyan.Render(inTok),
				styleCyan.Render(outTok),
			)
			sb.WriteString(detail + "\n")
		} else {
			sb.WriteString(styleDim.Render("  select a row to see details") + "\n")
		}
	}

	// ── Launch input ───────────────────────────────────────────────────────────
	if m.launching {
		sb.WriteString("\n")
		prompt := fmt.Sprintf("  %s [%s]  enter to confirm, esc to cancel",
			sectionLabel("launch in:"),
			m.launchInput.View(),
		)
		sb.WriteString(prompt + "\n")
	}

	// ── Footer ─────────────────────────────────────────────────────────────────
	sb.WriteString("\n")
	footerItems := []string{"f focus", "ctrl-] detach", "K kill", "L launch", "↑/↓ navigate"}
	var footerParts []string
	for _, item := range footerItems {
		footerParts = append(footerParts, styleDim.Render(item))
	}
	sb.WriteString("  " + strings.Join(footerParts, "  ·  ") + "\n")

	return sb.String()
}

// liveUptime returns a short human-readable uptime for a live process start time.
func liveUptime(t time.Time) string {
	if t.IsZero() {
		return "─"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// min6 returns the smaller of n and 6.
func min6(n int) int {
	if n < 6 {
		return n
	}
	return 6
}
