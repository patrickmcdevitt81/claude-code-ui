// Package tui provides the Bubble Tea model for Cockpit's terminal user interface.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Hex palette — matches the "Professional Web Dashboard" reference design.
var (
	// Background layers
	colorPanel    = lipgloss.Color("#0a0a0b") // panel / card background
	colorPanelAlt = lipgloss.Color("#0c0c0e") // tab-strip background

	// Brand accent — Claude terracotta (#d97757 from the reference design)
	colorAccent = lipgloss.Color("#d97757")

	// Text hierarchy
	colorText      = lipgloss.Color("#e6e6e6") // primary
	colorTextDim   = lipgloss.Color("#8a8a93") // secondary / labels
	colorTextMuted = lipgloss.Color("#5a5a63") // timestamps / hints

	// Borders
	colorBorder       = lipgloss.Color("#1f1f24") // subtle panel separator
	colorBorderActive = lipgloss.Color("#2a2a30") // selection-highlight background

	// Semantic
	colorGreen  = lipgloss.Color("#6bbf73") // pass / busy
	colorRed    = lipgloss.Color("#d4183d") // error / fail
	colorCost   = lipgloss.Color("#d9a557") // cost / money
	colorTeal   = lipgloss.Color("#5fb3b3") // sparklines / chart blue-green
	colorPurple = lipgloss.Color("#9b7cc8") // sessions accent
)

// Backward-compatibility aliases so existing view code keeps compiling
// without change — only the rendered colours change.
var (
	colorOrange = colorAccent
	colorAmber  = colorAccent
	colorWhite  = colorText
	colorDim    = colorTextMuted
	colorGray   = colorTextDim
	colorCyan   = colorTeal
	colorMint   = colorGreen
	colorYellow = colorCost
)

var (
	// Text styles.
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	styleDim    = lipgloss.NewStyle().Foreground(colorTextMuted)
	styleGray   = lipgloss.NewStyle().Foreground(colorTextDim)
	styleBusy   = lipgloss.NewStyle().Foreground(colorGreen)
	styleIdle   = lipgloss.NewStyle().Foreground(colorTextMuted)
	styleCost   = lipgloss.NewStyle().Foreground(colorCost)
	styleError  = lipgloss.NewStyle().Foreground(colorRed)
	styleOrange = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleAmber  = lipgloss.NewStyle().Foreground(colorAccent)
	styleCyan   = lipgloss.NewStyle().Foreground(colorTeal)
	styleMint   = lipgloss.NewStyle().Foreground(colorGreen)
	stylePurple = lipgloss.NewStyle().Foreground(colorPurple)

	// Border — one unified subtle style; per-view identity comes from the
	// chrome tab strip, not from per-view border colours.
	styleBorder       = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorBorder)
	styleBorderCyan   = styleBorder // compat alias
	styleBorderGreen  = styleBorder // compat alias
	styleBorderPurple = styleBorder // compat alias
	styleBorderDim    = styleBorder // compat alias
)

// Animation frame sequences — index into these with (animFrame % len(seq)).
var (
	// Braille spinner used for busy/running states.
	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	// Spark — cycles inside the Claude "C" logo.
	sparkFrames = []string{"✦", "✧", "⋆", "✧", "✦", "★", "✦", "✧"}
	// Pulse dot — shown next to busy agents.
	pulseFrames = []string{"●", "◉", "●", "◉", "○", "◉"}
)

// frame returns seq[i%len(seq)], safe for any non-negative i.
func frame(seq []string, i int) string {
	if len(seq) == 0 {
		return ""
	}
	return seq[i%len(seq)]
}

// sparklineChar maps a fractional value in [0,1] to a Unicode bar character.
func sparklineChar(frac float64) string {
	chars := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	idx := int(frac*float64(len(chars)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(chars) {
		idx = len(chars) - 1
	}
	return chars[idx]
}

// sectionLabel returns a styled, muted uppercase section heading.
func sectionLabel(s string) string {
	return lipgloss.NewStyle().Foreground(colorTextDim).Render(s)
}

// fillLine pads s to exactly width visible columns and applies bg as the
// background colour. Existing ANSI foreground/background codes inside s are
// preserved — lipgloss re-asserts the outer bg after any reset inside s.
func fillLine(s string, width int, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Width(width).Background(bg).Render(s)
}

// padBlock applies fillLine to every line of body so each is exactly width
// columns wide with the bg background colour.
func padBlock(body string, width int, bg lipgloss.Color) string {
	lines := strings.Split(body, "\n")
	// Strip the trailing empty string that Split leaves after a final "\n".
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fillLine(l, width, bg)
	}
	return strings.Join(out, "\n")
}

// cropBody pads every line of body to width and caps / pads to exactly bodyH
// rows so the chrome always occupies the full terminal height.
func cropBody(body string, width, bodyH int, bg lipgloss.Color) string {
	lines := strings.Split(body, "\n")
	// Strip trailing empty string from final "\n".
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	// Cap at bodyH.
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	out := make([]string, bodyH)
	for i := 0; i < bodyH; i++ {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fillLine(l, width, bg)
	}
	return strings.Join(out, "\n")
}
