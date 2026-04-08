package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Status bar (top).
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)

	// User message bubble.
	userMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Bold(true)

	// User prompt marker.
	userPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true)

	// Assistant message body.
	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	// Assistant message header (tool summary line).
	assistantHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("6")).
				Bold(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(lipgloss.Color("8"))

	// System message (slash command results, errors).
	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true)

	// Tool call: running.
	toolRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("6"))

	// Tool call: completed.
	toolDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	// Tool call: error.
	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))

	// Input area border.
	inputBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(0, 1)

	// Input area border when focused/active.
	inputActiveBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(0, 1)

	// Error text.
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)

	// Dim text (for secondary information).
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)
