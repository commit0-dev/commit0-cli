package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderConversation builds the full conversation viewport content
// with styled message bubbles, tool summaries, and streaming indicators.
func (m *Model) renderConversation() string {
	var sb strings.Builder
	width := m.width - 2
	if width < 20 {
		width = 80
	}

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			sb.WriteString(renderUserMessage(msg, width))
		case "assistant":
			sb.WriteString(renderAssistantMessage(msg, width))
		case "system":
			sb.WriteString(renderSystemMessage(msg, width))
		}
		sb.WriteString("\n")
	}

	// Render active streaming state.
	if m.streaming {
		sb.WriteString(renderStreamingState(m, width))
	}

	return sb.String()
}

// renderUserMessage renders a user message with a colored prompt marker.
func renderUserMessage(msg ChatMessage, _ int) string {
	var sb strings.Builder
	timeStr := msg.Time.Format("15:04")
	header := userPromptStyle.Render("> ") + userMsgStyle.Render(msg.Content)
	sb.WriteString(header)
	sb.WriteString(dimStyle.Render("  " + timeStr))
	sb.WriteString("\n")
	return sb.String()
}

// renderAssistantMessage renders an assistant response with tool summary
// and markdown-rendered body inside a styled block.
func renderAssistantMessage(msg ChatMessage, width int) string {
	var sb strings.Builder

	// Tool summary header.
	if len(msg.Tools) > 0 {
		totalDuration := time.Duration(0)
		for _, t := range msg.Tools {
			totalDuration += t.Duration
		}

		header := fmt.Sprintf("commit0  %d tools, %.1fs",
			len(msg.Tools), totalDuration.Seconds())
		sb.WriteString(assistantHeaderStyle.Width(width).Render(header))
		sb.WriteString("\n")

		// Individual tool lines.
		for _, tool := range msg.Tools {
			icon := toolDoneStyle.Render("  ✓")
			name := lipgloss.NewStyle().Bold(true).Render(tool.Name)
			dur := ""
			if tool.Duration > 0 {
				dur = dimStyle.Render(fmt.Sprintf(" (%dms)", tool.Duration.Milliseconds()))
			}
			sb.WriteString(fmt.Sprintf("%s %s%s\n", icon, name, dur))
		}
		sb.WriteString("\n")
	}

	// Markdown body.
	rendered := renderMarkdown(msg.Content)
	if rendered != "" {
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderSystemMessage renders system messages (slash command results, errors) dimmed.
func renderSystemMessage(msg ChatMessage, _ int) string {
	rendered := renderMarkdown(msg.Content)
	if rendered == "" {
		rendered = msg.Content
	}
	return systemMsgStyle.Render(rendered) + "\n"
}

// renderStreamingState renders the current tool activity and pending text
// during agent streaming.
func renderStreamingState(m *Model, _ int) string {
	var sb strings.Builder

	// Completed tools.
	for _, tool := range m.toolHistory {
		if tool.Status == "done" {
			icon := toolDoneStyle.Render("  ✓")
			name := lipgloss.NewStyle().Bold(true).Render(tool.Name)
			dur := ""
			if tool.Duration > 0 {
				dur = dimStyle.Render(fmt.Sprintf(" (%dms)", tool.Duration.Milliseconds()))
			}
			sb.WriteString(fmt.Sprintf("%s %s%s\n", icon, name, dur))
		}
	}

	// Currently running tool.
	for _, tool := range m.toolHistory {
		if tool.Status == "running" {
			sb.WriteString(toolRunningStyle.Render(
				fmt.Sprintf("  %s %s", m.spinner.View(), tool.Name)))
			if tool.Args != "" {
				sb.WriteString(dimStyle.Render(fmt.Sprintf(": %s", tool.Args)))
			}
			sb.WriteString("\n")
		}
	}

	// Pending response text (being streamed).
	if len(m.pendingChunks) > 0 {
		sb.WriteString("\n")
		text := strings.Join(m.pendingChunks, "")
		sb.WriteString(assistantMsgStyle.Render(text))
		sb.WriteString("\n")
	} else if m.currentTool == "thinking" && len(m.toolHistory) == 0 {
		// Agent is thinking but hasn't started any tools yet.
		sb.WriteString(toolRunningStyle.Render(
			fmt.Sprintf("  %s thinking...", m.spinner.View())))
		sb.WriteString("\n")
	}

	return sb.String()
}
