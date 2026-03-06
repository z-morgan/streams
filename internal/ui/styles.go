package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("12")  // bright blue
	colorSecondary = lipgloss.Color("243") // gray
	colorSuccess   = lipgloss.Color("10")  // green
	colorError     = lipgloss.Color("9")   // red
	colorWarning   = lipgloss.Color("11")  // yellow
	colorMuted     = lipgloss.Color("240") // dark gray

	// Status colors
	statusColors = map[string]lipgloss.Color{
		"Created": colorSecondary,
		"Running": colorPrimary,
		"Paused":  colorWarning,
		"Stopped": colorMuted,
	}

	// Dashboard styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	normalRowStyle = lipgloss.NewStyle()

	cursorStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	// Detail view styles
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(colorSecondary).
			MarginBottom(1)

	snapshotSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	snapshotNormalStyle = lipgloss.NewStyle().
				Foreground(colorSecondary)

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	errorBlockStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorError).
			Padding(0, 1)

	// Guidance overlay
	overlayStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 2)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Tail view styles
	toolLineStyle = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)
)
