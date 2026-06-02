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
// - DEAD : the done channel is already closed (process exited).
// - BUSY : the agent's session ID appears in m.processes with status=="busy".
// - IDLE : alive but not currently processing a prompt.
func classifyAgent(a *agent.Agent, m Model) agentStatus {
	// Check if the process has already exited.
	select {
	case <-a.Wait():
		return statusDead
	default:
	}

	// Look up session in live processes.
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
	// Alive but session not yet detected (or process idle).
	return statusIdle
}

// lookupSession returns the store.Session for an agent, or nil if not found.
func lookupSession(a *agent.Agent, m Model) *sessionInfo {
	sid := a.GetSessionID()
	if sid == "" {
		return nil
	}
	for i := range m.sessions {
		if m.sessions[i].SessionID == sid {
			return &sessionInfo{
				model:      m.sessions[i].Model,
				costUSD:    m.sessions[i].CostUSD,
				inTokens:   m.sessions[i].TotalInputTokens,
				outTokens:  m.sessions[i].TotalOutputTokens,
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

// sortedAgents returns the agent list in a stable order (by ID) so that
// row indices are deterministic across renders.
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
	styleDead     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("255"))
	styleNewRow = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
)

// renderAgents builds the full agents-view string.
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
	busyCount := 0
	idleCount := 0
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

	// ── Header box ─────────────────────────────────────────────────────────────
	summary := fmt.Sprintf(" %d running  │  %d idle  │  $%.2f session cost",
		busyCount, idleCount, totalCost)
	sb.WriteString(styleBorder.Width(w - 4).Render(
		styleHeader.Render("─ MISSION CONTROL") + "  " + summary,
	))
	sb.WriteString("\n\n")

	// ── Column header ──────────────────────────────────────────────────────────
	colHeader := fmt.Sprintf("  %-8s  %-10s  %-30s  %-8s  %-20s",
		"ID", "STATUS", "CWD", "COST", "SESSION")
	sb.WriteString(styleDim.Render(colHeader) + "\n")

	// ── Agent rows ─────────────────────────────────────────────────────────────
	for i, a := range agents {
		status := classifyAgent(a, m)
		si := lookupSession(a, m)

		// Raw (no-ANSI) status strings for correct width math in Sprintf.
		statusEmoji := map[agentStatus]string{statusBusy: "●", statusIdle: "○", statusDead: "✕"}[status]
		statusWord := map[agentStatus]string{statusBusy: "BUSY ", statusIdle: "IDLE ", statusDead: "DEAD "}[status]

		idShort := a.ID
		if len(idShort) > 6 {
			idShort = idShort[:6]
		}

		cwdTrunc := truncate(a.CWD, 30)

		costStr := "─"
		modelStr := "─"
		if si != nil {
			costStr = fmt.Sprintf("$%.2f", si.costUSD)
			modelStr = truncate(si.model, 20)
		}

		// Build the full line with raw strings so %-Ns pads by visible width,
		// then apply a single color/highlight to the assembled line.
		rawLine := fmt.Sprintf("  %-6s  %s %-5s  %-30s  %-8s  %-20s",
			idShort, statusEmoji, statusWord, cwdTrunc, costStr, modelStr,
		)

		var renderedLine string
		if i == m.selectedAgentIdx {
			renderedLine = styleSelected.Render(rawLine)
		} else {
			switch status {
			case statusBusy:
				renderedLine = styleBusy.Render(rawLine)
			case statusDead:
				renderedLine = styleError.Render(rawLine)
			default:
				renderedLine = styleDim.Render(rawLine)
			}
		}
		sb.WriteString(renderedLine)
		sb.WriteString("\n")
	}

	// ── [new] row ──────────────────────────────────────────────────────────────
	newRowIdx := len(agents)
	newRowText := "  [new]    + LAUNCH  (press L to launch in a new directory)"
	if newRowIdx == m.selectedAgentIdx {
		sb.WriteString(styleSelected.Render(newRowText))
	} else {
		sb.WriteString(styleNewRow.Render(newRowText))
	}
	sb.WriteString("\n")

	// ── Detail pane ────────────────────────────────────────────────────────────
	sb.WriteString("\n")
	if m.selectedAgentIdx < len(agents) {
		a := agents[m.selectedAgentIdx]
		si := lookupSession(a, m)

		modelName := "─"
		inTok := "─"
		outTok := "─"
		if si != nil {
			modelName = si.model
			inTok = formatTokens(si.inTokens)
			outTok = formatTokens(si.outTokens)
		}

		detail := fmt.Sprintf("  AGENT: %s   %s   %s   in_tokens=%s  out_tokens=%s",
			a.ID[:min6(len(a.ID))],
			truncate(a.CWD, 40),
			modelName,
			inTok,
			outTok,
		)
		sb.WriteString(styleHeader.Render(detail) + "\n")
	} else {
		sb.WriteString(styleDim.Render("  (select an agent row to see details; L to launch)") + "\n")
	}

	// ── Launch input (shown when L is pressed) ──────────────────────────────
	if m.launching {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  Launch agent in: [%s]  enter to confirm, esc to cancel\n",
			m.launchInput.View()))
	}

	// ── Separator + footer ──────────────────────────────────────────────────────
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render(strings.Repeat("─", w)) + "\n")
	sb.WriteString(styleDim.Render(
		"  ↑/↓ select   f focus   K kill   L launch   tab → dashboard   1 dash   2 agents   q quit",
	) + "\n")

	return sb.String()
}

// min6 returns the smaller of n and 6.
func min6(n int) int {
	if n < 6 {
		return n
	}
	return 6
}
