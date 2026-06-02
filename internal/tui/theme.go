// Package tui provides the Bubble Tea model for Cockpit's terminal user interface.
package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorGreen  = lipgloss.Color("82")
	colorRed    = lipgloss.Color("196")
	colorYellow = lipgloss.Color("220")
	colorDim    = lipgloss.Color("240")
	colorWhite  = lipgloss.Color("255")

	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
	styleBusy   = lipgloss.NewStyle().Foreground(colorGreen)
	styleIdle   = lipgloss.NewStyle().Foreground(colorDim)
	styleCost   = lipgloss.NewStyle().Foreground(colorYellow)
	styleError  = lipgloss.NewStyle().Foreground(colorRed)
	styleBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
)
