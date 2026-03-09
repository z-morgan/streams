package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("33")  // blue
	colorSecondary = lipgloss.Color("243") // gray
	colorSuccess   = lipgloss.Color("34")  // green
	colorError     = lipgloss.Color("160") // red
	colorWarning   = lipgloss.Color("214") // amber
	colorMuted     = lipgloss.Color("240") // dark gray
	colorSubtle    = lipgloss.Color("236") // near-background gray
	colorHighlight = lipgloss.Color("39")  // cyan accent

	// Status colors
	statusColors = map[string]lipgloss.Color{
		"Created":   colorSecondary,
		"Running":   colorPrimary,
		"Paused":    colorWarning,
		"Stopped":   colorMuted,
		"Completed": colorSuccess,
	}

	// Status bar styles
	topBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Background(colorSubtle).
			PaddingLeft(1).
			PaddingRight(1)

	bottomBarStyle = lipgloss.NewStyle().
			Background(colorSubtle).
			PaddingLeft(1).
			PaddingRight(1)

	// Dashboard styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary).
				Background(colorSubtle)

	normalRowStyle = lipgloss.NewStyle()

	metadataStyle = lipgloss.NewStyle().
			Faint(true)

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

	// errorBarStyle is used for inline error messages in the status bar.
	errorBarStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true).
			Background(lipgloss.Color("52")). // dark red
			PaddingLeft(1).
			PaddingRight(1)

	// errorBlockStyle is used for structured error blocks in detail views.
	errorBlockStyle = lipgloss.NewStyle().
			Foreground(colorError).
			BorderStyle(lipgloss.Border{Left: "▌"}).
			BorderLeft(true).
			BorderForeground(colorError).
			PaddingLeft(1)

	colorOverlayBg = lipgloss.Color("234") // overlay background

	overlayTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	// Guidance overlay
	overlayStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Background(colorOverlayBg).
			Padding(1, 2)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorHighlight).
			Bold(true)

	helpActionStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	helpSepStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	// Tail view styles
	toolLineStyle = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	// Channel view styles
	dashedVerticalBorder = lipgloss.Border{
		Left:  "┊",
		Right: "┊",
	}

	channelBorderStyle = lipgloss.NewStyle().
				BorderStyle(dashedVerticalBorder).
				BorderLeft(true).
				BorderRight(true).
				BorderTop(false).
				BorderBottom(false).
				BorderForeground(colorMuted).
				PaddingLeft(1).
				PaddingRight(1)

	channelSelectedBorderStyle = lipgloss.NewStyle().
					BorderStyle(dashedVerticalBorder).
					BorderLeft(true).
					BorderRight(true).
					BorderTop(false).
					BorderBottom(false).
					BorderForeground(colorPrimary).
					PaddingLeft(1).
					PaddingRight(1)

	channelSepStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginBottom(1)

	channelHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	channelHeaderMutedStyle = lipgloss.NewStyle().
				Foreground(colorSecondary)

	iterRowStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	iterRowErrorStyle = lipgloss.NewStyle().
				Foreground(colorError)

	inProgressStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	// Spinner frames — braille dots cycle, similar to Claude Code's thinking indicator.
	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)
