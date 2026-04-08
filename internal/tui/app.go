package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ChatMessage represents one message in the conversation.
type ChatMessage struct {
	Role    string     // "user", "assistant", "system"
	Content string     // raw content (markdown for assistant)
	Tools   []ToolCall // tool calls made during this turn
	Time    time.Time
}

// ToolCall tracks a single tool invocation.
type ToolCall struct {
	Name     string
	Args     string
	Result   string
	Duration time.Duration
	Status   string // "running", "done", "error"
	Start    time.Time
}

// Services holds all commit0 services needed by the TUI.
type Services struct {
	Agent      domain.AgentRunner
	Store      domain.GraphStore
	Trace      *app.TraceService
	Blast      *app.BlastService
	Index      *app.IndexService
	Query      *app.QueryService
	Flow       *app.FieldFlowService
	APISurface *app.APISurfaceService
	Cfg        *config.Config
	Cleanup    func()
}

// Model is the root Bubble Tea model for the commit0 TUI.
type Model struct {
	// Services (persist for session lifetime).
	services *Services
	cfg      *config.Config
	cleanup  func()
	program  *tea.Program

	// Conversation.
	messages  []ChatMessage
	repoSlug  string
	sessionID string

	// UI components.
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	// Streaming state.
	streaming     bool
	currentTool   string
	pendingChunks []string
	toolHistory   []ToolCall

	// Layout.
	width, height int
	ready         bool

	// Error.
	err error
}

// NewModel creates a new TUI model with the given services.
func NewModel(svcs *Services, repoSlug string) Model {
	// Input textarea.
	ti := textarea.New()
	ti.Placeholder = "Ask about your codebase... (Enter to send, Ctrl+J for newline)"
	ti.Focus()
	ti.SetHeight(3)
	ti.ShowLineNumbers = false
	ti.KeyMap.InsertNewline.SetKeys("ctrl+j")

	// Spinner for tool execution.
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = toolRunningStyle

	slug := repoSlug
	if slug == "" {
		slug = "(no repo)"
	}

	return Model{
		services: svcs,
		cfg:      svcs.Cfg,
		cleanup:  svcs.Cleanup,
		repoSlug: slug,
		input:    ti,
		spinner:  sp,
	}
}

// SetProgram stores the tea.Program reference for goroutine-based Send().
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.recalculateLayout()
		if !m.ready {
			m.ready = true
			initMarkdownRenderer(m.width)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+l":
			if !m.streaming {
				m.messages = nil
				m.toolHistory = nil
				m.pendingChunks = nil
				m.rebuildViewport()
			}
			return m, nil
		case "enter":
			if !m.streaming {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					m.input.Reset()
					return m.handleSubmit(text)
				}
			}
			return m, nil
		}
		// Don't forward keys to input during streaming.
		if m.streaming {
			return m, nil
		}

	// Agent streaming events.
	case AgentThinkingMsg:
		m.currentTool = "thinking"
		return m, m.spinner.Tick

	case AgentToolStartMsg:
		m.currentTool = msg.Name
		m.toolHistory = append(m.toolHistory, ToolCall{
			Name: msg.Name, Args: truncate(msg.Args, 80), Status: "running", Start: time.Now(),
		})
		m.rebuildViewport()
		return m, m.spinner.Tick

	case AgentToolResultMsg:
		for i := len(m.toolHistory) - 1; i >= 0; i-- {
			if m.toolHistory[i].Name == msg.Name && m.toolHistory[i].Status == "running" {
				m.toolHistory[i].Status = "done"
				m.toolHistory[i].Result = truncate(msg.Result, 120)
				m.toolHistory[i].Duration = time.Since(m.toolHistory[i].Start)
				break
			}
		}
		m.currentTool = ""
		m.rebuildViewport()
		return m, nil

	case AgentTextMsg:
		m.pendingChunks = append(m.pendingChunks, msg.Content)
		m.rebuildViewport()
		return m, nil

	case AgentDoneMsg:
		m.streaming = false
		m.currentTool = ""
		content := strings.Join(m.pendingChunks, "")
		if content != "" {
			m.messages = append(m.messages, ChatMessage{
				Role: "assistant", Content: content, Tools: m.toolHistory, Time: time.Now(),
			})
		}
		m.pendingChunks = nil
		m.toolHistory = nil
		m.rebuildViewport()
		return m, nil

	case slashResultMsg:
		m.streaming = false
		m.messages = append(m.messages, ChatMessage{
			Role: "system", Content: msg.content, Time: time.Now(),
		})
		m.rebuildViewport()
		return m, nil

	case AgentErrorMsg:
		m.streaming = false
		m.currentTool = ""
		m.err = msg.Err
		m.messages = append(m.messages, ChatMessage{
			Role: "system", Content: fmt.Sprintf("Error: %v", msg.Err), Time: time.Now(),
		})
		m.rebuildViewport()
		return m, nil

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Forward to sub-components.
	if !m.streaming {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing commit0 TUI..."
	}

	statusBar := m.renderStatusBar()
	conversation := m.viewport.View()
	inputArea := m.renderInputArea()

	return lipgloss.JoinVertical(lipgloss.Left,
		statusBar,
		conversation,
		inputArea,
	)
}

// handleSubmit processes user input (plain text or slash command).
func (m Model) handleSubmit(text string) (Model, tea.Cmd) {
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// Add user message.
	m.messages = append(m.messages, ChatMessage{
		Role: "user", Content: text, Time: time.Now(),
	})
	m.streaming = true
	m.pendingChunks = nil
	m.toolHistory = nil
	m.rebuildViewport()

	// Launch agent in background goroutine.
	return m, m.startAgentChat(text)
}

// handleSlashCommand processes slash commands.
func (m Model) handleSlashCommand(input string) (Model, tea.Cmd) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return m, nil
	}

	switch parts[0] {
	case "/help":
		m.messages = append(m.messages, ChatMessage{
			Role: "system", Content: helpText(), Time: time.Now(),
		})
	case "/clear":
		m.messages = nil
	case "/repo":
		if len(parts) > 1 {
			m.repoSlug = parts[1]
		}
		m.messages = append(m.messages, ChatMessage{
			Role: "system", Content: fmt.Sprintf("Active repo: **%s**", m.repoSlug), Time: time.Now(),
		})
	case "/memory":
		m.addSystemMsg(m.buildMemoryDisplay())
	case "/compact":
		instructions := ""
		if len(parts) > 1 {
			instructions = strings.Join(parts[1:], " ")
		}
		mp := &m
		mp.compactConversation(instructions)
		mp.addSystemMsg("Context compacted.")
	case "/trace":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: `/trace <symbol>` [--reverse] [--depth N]")
		} else {
			m.streaming = true
			m.rebuildViewport()
			return m, m.runTraceCmd(parts[1:])
		}
	case "/blast":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: `/blast <symbol>` [--depth N]")
		} else {
			m.streaming = true
			m.rebuildViewport()
			return m, m.runBlastCmd(parts[1:])
		}
	case "/api":
		m.streaming = true
		m.rebuildViewport()
		return m, m.runAPIDiscoverCmd()
	case "/index":
		path := "."
		if len(parts) > 1 {
			path = parts[1]
		}
		m.addSystemMsg(fmt.Sprintf("Indexing `%s`... (this may take a moment)", path))
		m.streaming = true
		m.rebuildViewport()
		return m, m.runIndexCmd(path)
	case "/quit":
		return m, tea.Quit
	default:
		m.addSystemMsg(fmt.Sprintf("Unknown command: `%s` (type /help)", parts[0]))
	}

	m.rebuildViewport()
	return m, nil
}

func (m *Model) addSystemMsg(content string) {
	m.messages = append(m.messages, ChatMessage{
		Role: "system", Content: content, Time: time.Now(),
	})
}

// runTraceCmd executes /trace <symbol> as an async command.
func (m Model) runTraceCmd(args []string) tea.Cmd {
	svc := m.services.Trace
	repoSlug := m.repoSlug
	symbol := args[0]
	direction := "forward"
	depth := 6

	for i, arg := range args {
		if arg == "--reverse" {
			direction = "reverse"
		}
		if arg == "--depth" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &depth)
		}
	}

	return func() tea.Msg {
		if svc == nil {
			return AgentErrorMsg{Err: fmt.Errorf("trace service not available")}
		}
		ctx := context.Background()
		result, err := svc.Trace(ctx, app.TraceRequest{
			Symbol:    symbol,
			RepoSlug:  repoSlug,
			Direction: direction,
			Depth:     depth,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("trace: %w", err)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("**Trace %s** from `%s` (%dms)\n\n", direction, symbol, result.Timing.TotalMS))
		for _, hop := range result.Tree {
			renderTraceHop(&sb, hop, 0)
		}
		if result.Explanation != "" {
			sb.WriteString("\n" + result.Explanation)
		}

		return slashResultMsg{content: sb.String()}
	}
}

// runBlastCmd executes /blast <symbol> as an async command.
func (m Model) runBlastCmd(args []string) tea.Cmd {
	svc := m.services.Blast
	repoSlug := m.repoSlug
	symbol := args[0]
	maxDepth := 6

	for i, arg := range args {
		if arg == "--depth" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &maxDepth)
		}
	}

	return func() tea.Msg {
		if svc == nil {
			return AgentErrorMsg{Err: fmt.Errorf("blast service not available")}
		}
		ctx := context.Background()
		result, err := svc.Blast(ctx, app.BlastRequest{
			Symbol:   symbol,
			RepoSlug: repoSlug,
			MaxDepth: maxDepth,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("blast: %w", err)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("**Blast radius** for `%s`: **%d** affected nodes (%dms)\n\n",
			symbol, len(result.Affected), result.Timing.TotalMS))
		for i, a := range result.Affected {
			sb.WriteString(fmt.Sprintf("%d. `%s` (hop %d) — %s\n",
				i+1, a.Node.Qualified, a.HopCount, a.Node.FilePath))
		}
		if result.Summary != "" {
			sb.WriteString("\n" + result.Summary)
		}

		return slashResultMsg{content: sb.String()}
	}
}

// runAPIDiscoverCmd executes /api discover as an async command.
func (m Model) runAPIDiscoverCmd() tea.Cmd {
	svc := m.services.APISurface
	repoSlug := m.repoSlug

	return func() tea.Msg {
		if svc == nil {
			return AgentErrorMsg{Err: fmt.Errorf("API surface service not available")}
		}
		ctx := context.Background()
		surface, err := svc.Discover(ctx, repoSlug)
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("api discover: %w", err)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("**API Surface**: %d endpoints (%dms)\n\n",
			len(surface.Endpoints), surface.Timing.TotalMS))

		sb.WriteString("| Method | Path | Handler | Auth |\n")
		sb.WriteString("|--------|------|---------|------|\n")
		for _, ep := range surface.Endpoints {
			auth := "NONE"
			if len(ep.AuthChain) > 0 {
				auth = strings.Join(ep.AuthChain, ", ")
			}
			sb.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n",
				ep.Endpoint.Method, ep.Endpoint.Path, ep.Endpoint.Handler, auth))
		}

		noAuth := 0
		for _, ep := range surface.Endpoints {
			if len(ep.AuthChain) == 0 {
				noAuth++
			}
		}
		if noAuth > 0 {
			sb.WriteString(fmt.Sprintf("\n**%d endpoint(s)** without authentication middleware\n", noAuth))
		}

		return slashResultMsg{content: sb.String()}
	}
}

// runIndexCmd executes /index <path> as an async command.
func (m Model) runIndexCmd(repoPath string) tea.Cmd {
	svc := m.services.Index
	program := m.program

	return func() tea.Msg {
		if svc == nil {
			return AgentErrorMsg{Err: fmt.Errorf("index service not available")}
		}
		ctx := context.Background()
		result, err := svc.Index(ctx, app.IndexRequest{
			RepoPath: repoPath,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("index: %w", err)}
		}

		content := fmt.Sprintf("**Indexed** `%s`: %d files, %d nodes (%dms)",
			repoPath, result.FilesIndexed, result.NodesCreated, result.Timing.TotalMS)

		if program != nil {
			program.Send(slashResultMsg{content: content})
		}
		return AgentDoneMsg{}
	}
}

// renderTraceHop renders a trace hop with indentation.
func renderTraceHop(sb *strings.Builder, hop types.TraceHop, depth int) {
	indent := strings.Repeat("  ", depth)
	sb.WriteString(fmt.Sprintf("%s- `%s` (%s:%d)\n",
		indent, hop.Node.Qualified, hop.Node.FilePath, hop.Node.StartLine))
	for _, child := range hop.Children {
		renderTraceHop(sb, child, depth+1)
	}
}

// startAgentChat launches the agent chat in a goroutine and streams events via Program.Send().
func (m Model) startAgentChat(message string) tea.Cmd {
	agent := m.services.Agent
	repoSlug := m.repoSlug
	sessionID := m.sessionID
	program := m.program

	return func() tea.Msg {
		if agent == nil {
			return AgentErrorMsg{Err: fmt.Errorf("agent not available (check GEMINI_API_KEY or OLLAMA_MODEL)")}
		}

		ctx := context.Background()
		events, err := agent.Chat(ctx, domain.ChatRequest{
			SessionID: sessionID,
			RepoSlug:  repoSlug,
			Message:   message,
		})
		if err != nil {
			return AgentErrorMsg{Err: err}
		}

		// Stream events to the TUI via Program.Send().
		for event := range events {
			if program == nil {
				continue
			}
			switch event.Type {
			case "thinking":
				program.Send(AgentThinkingMsg{Content: event.Content})
			case "tool_call":
				program.Send(AgentToolStartMsg{Name: event.ToolName, Args: event.Content})
			case "tool_result":
				program.Send(AgentToolResultMsg{Name: event.ToolName, Result: event.Content})
			case "message":
				program.Send(AgentTextMsg{Content: event.Content})
			case "error":
				program.Send(AgentErrorMsg{Err: fmt.Errorf("%s", event.Content)})
			}
		}

		return AgentDoneMsg{}
	}
}

// recalculateLayout adjusts component sizes based on terminal dimensions.
func (m *Model) recalculateLayout() {
	statusBarHeight := 1
	inputHeight := 5 // textarea + border
	viewportHeight := m.height - statusBarHeight - inputHeight

	if viewportHeight < 1 {
		viewportHeight = 1
	}

	m.viewport.Width = m.width
	m.viewport.Height = viewportHeight
	m.input.SetWidth(m.width - 4) // account for border padding
}

// rebuildViewport renders all messages and tool activity into the viewport.
func (m *Model) rebuildViewport() {
	m.viewport.SetContent(m.renderConversation())
	m.viewport.GotoBottom()
}

// renderStatusBar builds the top status bar.
func (m Model) renderStatusBar() string {
	left := fmt.Sprintf(" commit0 │ repo: %s", m.repoSlug)

	model := "gemini-2.5-flash"
	if m.cfg != nil && m.cfg.Gemini.ExplainModel != "" {
		model = m.cfg.Gemini.ExplainModel
	}
	tokens := m.estimateTokens()
	tokenStr := fmt.Sprintf("%dK", tokens/1000)
	if tokens < 1000 {
		tokenStr = fmt.Sprintf("%d", tokens)
	}
	right := fmt.Sprintf("%s │ %d msgs │ ~%s tokens ", model, len(m.messages), tokenStr)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return statusBarStyle.Width(m.width).Render(bar)
}

// renderInputArea builds the bottom input area.
func (m Model) renderInputArea() string {
	if m.streaming {
		indicator := m.spinner.View()
		label := m.currentTool
		if label == "" {
			label = "processing"
		}
		return fmt.Sprintf("  %s %s...", indicator, label)
	}

	style := inputActiveBorderStyle
	return style.Width(m.width - 2).Render(m.input.View())
}

// estimateTokens estimates the token count of the current conversation.
// Uses the rough heuristic of 1 token per 4 characters.
func (m Model) estimateTokens() int {
	total := 0
	for _, msg := range m.messages {
		total += len(msg.Content) / 4
		for _, tool := range msg.Tools {
			total += len(tool.Args)/4 + len(tool.Result)/4
		}
	}
	return total
}

// buildMemoryDisplay renders the memory budget and conversation statistics.
func (m Model) buildMemoryDisplay() string {
	tokens := m.estimateTokens()
	msgCount := len(m.messages)
	userMsgs := 0
	assistantMsgs := 0
	toolCalls := 0
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			userMsgs++
		case "assistant":
			assistantMsgs++
			toolCalls += len(msg.Tools)
		}
	}

	var sb strings.Builder
	sb.WriteString("**Memory & Context**\n\n")
	sb.WriteString(fmt.Sprintf("Estimated tokens: **%d**\n", tokens))
	sb.WriteString(fmt.Sprintf("Messages: %d (%d user, %d assistant)\n", msgCount, userMsgs, assistantMsgs))
	sb.WriteString(fmt.Sprintf("Tool calls: %d\n", toolCalls))
	sb.WriteString(fmt.Sprintf("Repo: `%s`\n", m.repoSlug))

	if tokens > 10000 {
		sb.WriteString("\nContext is getting large. Consider `/compact` to compress history.\n")
	}

	return sb.String()
}

// compactConversation compresses older messages to reduce context size.
// Keeps the last 4 messages intact and summarizes older ones.
func (m *Model) compactConversation(instructions string) { //nolint:unparam
	if len(m.messages) <= 4 {
		return
	}

	// Keep last 4 messages, summarize the rest.
	older := m.messages[:len(m.messages)-4]
	recent := m.messages[len(m.messages)-4:]

	var summaryParts []string
	for _, msg := range older {
		switch msg.Role {
		case "user":
			summaryParts = append(summaryParts, fmt.Sprintf("User asked: %s", truncate(msg.Content, 80)))
		case "assistant":
			summaryParts = append(summaryParts, fmt.Sprintf("Agent: %s", truncate(msg.Content, 120)))
		}
	}

	summary := strings.Join(summaryParts, " | ")
	if instructions != "" {
		summary = fmt.Sprintf("[Focus: %s] %s", instructions, summary)
	}

	compacted := []ChatMessage{{
		Role:    "system",
		Content: fmt.Sprintf("*Compacted %d earlier messages:* %s", len(older), summary),
		Time:    time.Now(),
	}}
	m.messages = append(compacted, recent...)
}

func helpText() string {
	return `**Available commands:**
- ` + "`/help`" + ` — show this help
- ` + "`/clear`" + ` — clear conversation
- ` + "`/repo <slug>`" + ` — switch repo context
- ` + "`/trace <symbol>`" + ` — trace call chain (add --reverse, --depth N)
- ` + "`/blast <symbol>`" + ` — blast radius analysis (add --depth N)
- ` + "`/api`" + ` — discover API endpoints
- ` + "`/index [path]`" + ` — index a repository
- ` + "`/memory`" + ` — show context and memory usage
- ` + "`/compact [focus]`" + ` — compress conversation history
- ` + "`/quit`" + ` — exit

**Keys:** Enter = send, Ctrl+J = newline, Ctrl+L = clear, Ctrl+C = quit`
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
