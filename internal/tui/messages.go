package tui

import "fmt"

// Agent streaming events (mapped from domain.ChatEvent).

// AgentThinkingMsg is sent when the agent is reasoning.
type AgentThinkingMsg struct{ Content string }

// AgentToolStartMsg is sent when the agent begins executing a tool.
type AgentToolStartMsg struct{ Name, Args string }

// AgentToolResultMsg is sent when a tool completes execution.
type AgentToolResultMsg struct{ Name, Result string }

// AgentTextMsg is sent for each chunk of the agent's text response.
type AgentTextMsg struct{ Content string }

// AgentDoneMsg is sent when the agent finishes its turn.
type AgentDoneMsg struct{}

// AgentErrorMsg is sent when the agent encounters an error.
type AgentErrorMsg struct{ Err error }

func (e AgentErrorMsg) Error() string { return fmt.Sprintf("agent error: %v", e.Err) }

// slashResultMsg is sent when a slash command completes with output.
type slashResultMsg struct{ content string }
