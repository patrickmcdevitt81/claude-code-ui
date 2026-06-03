package tui

import (
	"fmt"
	"sort"
	"strings"

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

// lookupSession returns the store.Session info for an agent, or nil.
func lookupSession(a *agent.Agent, m Model) *sessionInfo {
	sid := a.GetSessionID()
	if sid == "" {
		return nil
	}
	for i := range m.sessions {
		if m.sessions[i].SessionID == sid {
			return &sessionInfo{
				model:     m.sessions[i].Model,
				costUSD:   m.sessions[i].CostUSD,
				inTokens:  m.sessions[i].TotalInputTokens,
				outTokens: m.sessions[i].TotalOutputTokens,
			}
		}
	}
	return nil
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
func renderAgents(m Model) string {
	w := m.width
	if w < 72 {
		w = 72
	}

	var agents []*agent.Agent
	if m.manager != nil {
		agents = sortedAgents(m.manager.List())
	}

	// ── Tally ──────────────────────────────────────────────────────────────────
	busyCount, idleCount := 0, 0
	totalCost := 0.0
	for _, a := range agents {
		switch classifyAgent(a, m) {
		case statusBusy:
			busyCount++
		case statusIdle:
			idleCount++
		}
		if si := lookupSession(a, m); si != nil {
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

	// ── Column header ──────────────────────────────────────────────────────────
	const (
		cID  = 6
		cSt  = 5
		cUp  = 5
		cCWD = 32
		cCst = 7
		cSes = 16
	)
	hdr := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
		cID, "ID",
		cSt, "STATE",
		cUp, "UP",
		cCWD, "WORKING DIR",
		cCst, "COST",
		"MODEL/SESSION",
	)
	sb.WriteString(sectionLabel(hdr) + "\n")
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")

	// ── Agent rows ─────────────────────────────────────────────────────────────
	for i, a := range agents {
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

		uptime := a.UptimeStr()
		cwdShort := truncate(a.CWD, cCWD)

		costStr := "─"
		sessStr := "─"
		if si != nil {
			costStr = fmt.Sprintf("$%.2f", si.costUSD)
			sessStr = truncate(si.model, cSes)
		}

		// Focused indicator appended after session.
		focusMark := " "
		if a.ID == m.focusedID {
			focusMark = "●"
		}

		rawLine := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s %s",
			cID, idShort,
			cSt, stateWord,
			cUp, uptime,
			cCWD, cwdShort,
			cCst, costStr,
			cSes, sessStr,
			focusMark,
		)

		var renderedLine string
		if i == m.selectedAgentIdx {
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

	// ── [new] row ──────────────────────────────────────────────────────────────
	sb.WriteString(styleDim.Render("  "+strings.Repeat("─", w-4)) + "\n")
	newRowText := "  [new]   + LAUNCH   press L to launch in a custom directory"
	if len(agents) == m.selectedAgentIdx {
		sb.WriteString(styleSelected.Render(newRowText))
	} else {
		sb.WriteString(styleNewRow.Render(newRowText))
	}
	sb.WriteString("\n")

	// ── Detail pane for selected agent ─────────────────────────────────────────
	sb.WriteString("\n")
	if m.selectedAgentIdx < len(agents) {
		a := agents[m.selectedAgentIdx]
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

	// ── Launch input (shown when L is pressed) ─────────────────────────────────
	if m.launching {
		sb.WriteString("\n")
		prompt := fmt.Sprintf("  %s [%s]  enter to confirm, esc to cancel",
			sectionLabel("launch in:"),
			m.launchInput.View(),
		)
		sb.WriteString(prompt + "\n")
	}

	// ── Footer hint ────────────────────────────────────────────────────────────
	sb.WriteString("\n")
	footerItems := []string{
		"f focus",
		"ctrl-] detach",
		"K kill",
		"L launch",
		"↑/↓ navigate",
	}
	var footerParts []string
	for _, item := range footerItems {
		footerParts = append(footerParts, styleDim.Render(item))
	}
	sb.WriteString("  " + strings.Join(footerParts, "  ·  ") + "\n")

	return sb.String()
}

// min6 returns the smaller of n and 6.
func min6(n int) int {
	if n < 6 {
		return n
	}
	return 6
}
