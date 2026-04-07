package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/go-claw/claw/internal/commands"
	"github.com/go-claw/claw/internal/runtime"
)

// SessionState represents the current state of the TUI session.
type SessionState int

const (
	StateIdle SessionState = iota
	StateStreaming
	StateToolExec
)

// StreamTokenMsg carries an incremental streaming token.
type StreamTokenMsg struct {
	Text string
}

// ThinkingTokenMsg carries a thinking block token.
type ThinkingTokenMsg struct {
	Text string
}

// ToolStartMsg signals a tool has started executing.
type ToolStartMsg struct {
	Name  string
	Input string
}

// ToolDoneMsg signals a tool has finished executing.
type ToolDoneMsg struct {
	Name   string
	Result string
	Err    error
}

// DoneMsg signals the turn is complete.
type DoneMsg struct {
	Text  string
	Usage *runtime.TokenUsage
	Err   error
}

// StreamOutputMsg wraps a runtime TurnOutput for incremental display.
type StreamOutputMsg struct {
	Output runtime.TurnOutput
}

// TickMsg is sent periodically to update the spinner.
type TickMsg struct{}

// PermissionRequestMsg signals a permission prompt is needed.
type PermissionRequestMsg struct {
	ToolName string
	Input    string
}

// PermissionResponseMsg signals the user's response to a permission prompt.
type PermissionResponseMsg struct {
	Allow bool
}

// Model is the Bubbletea model for the Claw TUI.
type Model struct {
	state             SessionState
	rt                *runtime.ConversationRuntime
	compaction        runtime.CompactionConfig
	input             textarea.Model
	messages          []Message
	statusLine        string
	width             int
	height            int
	ctx               context.Context
	cancel            context.CancelFunc
	streaming         *StreamingState
	markdownStream    *MarkdownStreamState // incremental streaming renderer
	spinner           int
	scrollOffset       int
	vimMode           bool
	inputHistory      []string
	histIdx           int
	pendingPermission *PermissionRequestMsg
}

// Message represents one displayed message in the conversation.
type Message struct {
	Role      string // "user", "assistant", "tool", "system", "thinking"
	Content   string
	Time      time.Time
	Collapsed bool // for tool results that are collapsed by default
}

// NewModel creates a new TUI model.
func NewModel(rt *runtime.ConversationRuntime) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (/help for commands)"
	ta.Focus()
	ta.CharLimit = 10000
	ta.SetHeight(3)

	ctx, cancel := context.WithCancel(context.Background())


	return Model{
		state:      StateIdle,
		rt:         rt,
		compaction: runtime.DefaultCompactionConfig(),
		input:      ta,
		ctx:        ctx,
		cancel:     cancel,
		streaming:  NewStreamingState(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tickCmd())
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 4)
		m.scrollOffset = 0
		return m, nil


	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case TickMsg:
		m.spinner++
		return m, tickCmd()

	case StreamTokenMsg:
		m.streaming.AppendText(msg.Text)
		m.statusLine = m.spinnerText()
		return m, nil

	case ThinkingTokenMsg:
		m.streaming.AppendThinking(msg.Text)
		m.statusLine = m.spinnerText()
		return m, nil

	case ToolStartMsg:
		m.streaming.AddTool(msg.Name, msg.Input)
		m.state = StateToolExec
		m.statusLine = fmt.Sprintf("  Executing: %s", msg.Name)
		return m, nil

	case ToolDoneMsg:
		return m.handleToolDone(msg)

	case PermissionRequestMsg:
		m.pendingPermission = &msg
		m.state = StateIdle
		return m, nil

	case PermissionResponseMsg:
		m.pendingPermission = nil
		return m, nil

	case DoneMsg:
		return m.handleDoneMsg(msg)

	case streamOutput:
		return m.handleStreamOutput(msg)
	}

	// Update input area
	newInput, cmd := m.input.Update(msg)
	m.input = newInput
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		if m.state == StateStreaming {
			m.cancel()
			m.state = StateIdle
			m.statusLine = "Cancelled."
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyCtrlD:
		return m, tea.Quit

	case tea.KeyEsc:
		if m.state == StateStreaming {
			m.cancel()
			m.state = StateIdle
			m.statusLine = "Cancelled."
			return m, nil
		}
	}

	// PageUp/PageDown scrolling
	switch msg.String() {
	case "pgup":
		m.scrollOffset += m.height/2
		return m, nil
	case "pgdown":
		m.scrollOffset -= m.height/2; if m.scrollOffset < 0 { m.scrollOffset = 0 }
		return m, nil
	}

	// Vim mode keys (only in idle state, not streaming)
	if m.vimMode && m.state == StateIdle {
		switch msg.String() {
		case "j":
			m.scrollOffset++
			return m, nil
		case "k":
			m.scrollOffset--; if m.scrollOffset < 0 { m.scrollOffset = 0 }
			return m, nil
		case "g":
			m.scrollOffset = 999999
			return m, nil
		case "G":
			m.scrollOffset = 0
			return m, nil
		case "i":
			m.vimMode = false
			m.input.Focus()
			return m, nil
		}
	}

	// History navigation with Up/Down
	if m.state == StateIdle && m.input.Focused() {
		switch msg.String() {
		case "up":
			if len(m.inputHistory) > 0 && m.histIdx > 0 {
				m.histIdx--
				m.input.SetValue(m.inputHistory[m.histIdx])
			}
			return m, nil
		case "down":
			if m.histIdx < len(m.inputHistory)-1 {
				m.histIdx++
				m.input.SetValue(m.inputHistory[m.histIdx])
			} else {
				m.histIdx = len(m.inputHistory)
				m.input.Reset()
			}
			return m, nil
		}
	}

	// Enter to submit (only in idle state)
	if m.state == StateIdle && msg.String() == "enter" {
		if m.input.Focused() {
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				m.input.Reset()
				newM, cmd := m.handleSubmit(text)
				return newM, cmd
			}
		}
	}

	// Pass all other keys to the textarea for text input (only when idle)
	if m.state == StateIdle {
		newInput, _ := m.input.Update(msg)
		m.input = newInput
	}
	return m, nil
}

func (m Model) handleSubmit(text string) (Model, tea.Cmd) {
	// Save to input history
	m.inputHistory = append(m.inputHistory, text)
	m.histIdx = len(m.inputHistory)

	// Handle slash commands
	if strings.HasPrefix(text, "/") {
		m.messages = append(m.messages, Message{Role: "user", Content: text, Time: time.Now()})
		cmd, cmdArgs := commands.Parse(text)
		if cmd == nil {
			m.messages = append(m.messages, Message{Role: "system", Content: "Unknown command: /" + cmdArgs, Time: time.Now()})
			return m, nil
		}
		if cmd.Name == "quit" || cmd.Name == "exit" {
			return m, tea.Quit
		}
		output, err := cmd.Handler(cmdArgs)
		if err != nil {
			m.messages = append(m.messages, Message{Role: "system", Content: "error: " + err.Error(), Time: time.Now()})
		} else if output != "" {
			m.messages = append(m.messages, Message{Role: "system", Content: output, Time: time.Now()})
		}
		if cmd.Name == "compact" {
			result := m.rt.Compact(m.compaction)
			m.messages = append(m.messages, Message{
				Role:    "system",
				Content: fmt.Sprintf("Compacted: %d -> %d messages", result.MessagesBefore, result.MessagesAfter),
				Time:    time.Now(),
			})
		}
		if cmd.Name == "clear" {
			m.rt.Clear()
			m.messages = nil
			m.messages = append(m.messages, Message{Role: "system", Content: "Session cleared.", Time: time.Now()})
		}
		return m, nil
	}

	// Submit user message - start streaming turn
	m.messages = append(m.messages, Message{Role: "user", Content: text, Time: time.Now()})
	m.state = StateStreaming
	m.streaming = NewStreamingState()
		m.markdownStream = nil
	m.statusLine = m.spinnerText()

	return m, m.runTurn(text)
}

func (m Model) handleToolDone(msg ToolDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming.CompleteTool(msg.Name, msg.Result, msg.Err)
	if msg.Err != nil {
		m.messages = append(m.messages, Message{
			Role:      "tool",
			Content:   fmt.Sprintf("%s: error: %s", msg.Name, msg.Err.Error()),
			Time:      time.Now(),
			Collapsed: true,
		})
	} else {
		m.messages = append(m.messages, Message{
			Role:      "tool",
			Content:   fmt.Sprintf("%s: %s", msg.Name, truncate(msg.Result, 200)),
			Time:      time.Now(),
			Collapsed: true,
		})
	}
	pending := m.streaming.GetPendingTools()
	if len(pending) > 0 {
		m.statusLine = fmt.Sprintf("  Executing: %s", strings.Join(pending, ", "))
		return m, nil
	}
	m.state = StateStreaming
	m.statusLine = m.spinnerText()
	return m, nil
}

func (m Model) handleDoneMsg(msg DoneMsg) (tea.Model, tea.Cmd) {
	m.streaming.Finish(msg.Err)
	m.state = StateIdle
	m.input.Focus()

	if msg.Err != nil {
		m.messages = append(m.messages, Message{Role: "system", Content: "error: " + msg.Err.Error(), Time: time.Now()})
	} else if msg.Text != "" {
		m.messages = append(m.messages, Message{Role: "assistant", Content: msg.Text, Time: time.Now()})
	} else {
		// Fallback: read accumulated text from streaming state when DoneMsg has no Text
		if text := m.streaming.GetText(); text != "" {
			m.messages = append(m.messages, Message{Role: "assistant", Content: text, Time: time.Now()})
		}
	}


	if msg.Usage != nil {
		m.statusLine = lipgloss.NewStyle().Faint(true).Render(
			fmt.Sprintf("tokens: in=%d out=%d cache=%d", msg.Usage.InputTokens, msg.Usage.OutputTokens, msg.Usage.CacheCreationInputTokens+msg.Usage.CacheReadInputTokens))
	} else {
		m.statusLine = ""
	}
	return m, textarea.Blink
}

// handleStreamOutput processes incremental streaming output and re-schedules
// the next channel read for real-time display.
func (m Model) handleStreamOutput(msg streamOutput) (tea.Model, tea.Cmd) {
	out := msg.output
	switch out.Type {
	case "text_delta":
		m.streaming.AppendText(out.Text)
		m.statusLine = m.spinnerText()
	case "thinking_delta":
		m.streaming.AppendThinking(out.Text)
		m.statusLine = m.spinnerText()
	case "text":
		m.streaming.AppendText(out.Text)
		m.statusLine = m.spinnerText()
	case "thinking":
		m.streaming.AppendThinking(out.Text)
		m.statusLine = m.spinnerText()
	case "tool_use":
		inputJSON, _ := json.Marshal(out.ToolInput)
		m.streaming.AddTool(out.ToolName, string(inputJSON))
		m.state = StateToolExec
		m.statusLine = fmt.Sprintf("  Executing: %s", out.ToolName)
	case "tool_result":
		var result string
		if out.IsError {
			result = "error: " + out.Text
		} else {
			result = out.Text
		}
		m.streaming.CompleteTool(out.ToolName, result, nil)
		if out.IsError {
			m.messages = append(m.messages, Message{
				Role: "tool", Content: fmt.Sprintf("%s: %s", out.ToolName, result), Time: time.Now(), Collapsed: true,
			})
		} else {
			m.messages = append(m.messages, Message{
				Role: "tool", Content: fmt.Sprintf("%s: %s", out.ToolName, truncate(result, 200)), Time: time.Now(), Collapsed: true,
			})
		}
		m.state = StateStreaming
		m.statusLine = m.spinnerText()
	case "done":
		text := m.streaming.GetText()
		m.streaming.Finish(nil)
		m.state = StateIdle
		m.input.Focus()
		if text != "" {
			m.messages = append(m.messages, Message{Role: "assistant", Content: text, Time: time.Now()})
		}
		m.statusLine = ""
		return m, textarea.Blink
	}
	// Re-schedule the next stream read
	return m, waitForStreamOutput(msg.outCh, msg.usageCh, msg.errCh)
}

func (m Model) runTurn(prompt string) tea.Cmd {
	// Start the streaming turn in a goroutine. The channel reader pattern
	// below converts each TurnOutput into a Bubbletea message, enabling
	// real-time incremental display.
	outCh := make(chan runtime.TurnOutput, 64)
	usageCh := make(chan runtime.TokenUsage, 1)
	errCh := make(chan error, 1)

	go m.rt.RunTurnStreaming(m.ctx, prompt, outCh, usageCh, errCh)

	// Return the first "wait for stream output" command.
	return waitForStreamOutput(outCh, usageCh, errCh)
}

// streamOutput wraps a runtime TurnOutput plus the channels needed to
// schedule the next read after the UI processes this one.
type streamOutput struct {
	output  runtime.TurnOutput
	usageCh <-chan runtime.TokenUsage
	errCh   <-chan error
	outCh   <-chan runtime.TurnOutput
}

// waitForStreamOutput blocks until one of the streaming channels produces a
// value, then returns it as a message. This is the classic Bubbletea
// channel-reader pattern: each message re-schedules itself.
func waitForStreamOutput(outCh <-chan runtime.TurnOutput, usageCh <-chan runtime.TokenUsage, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case out, ok := <-outCh:
			if !ok {
				// Channel closed — turn is fully complete.
				// Check for final usage or error.
				select {
				case usage := <-usageCh:
					return DoneMsg{Usage: &usage}
				case err := <-errCh:
					return DoneMsg{Err: err}
				default:
					return DoneMsg{}
				}
			}
			return streamOutput{output: out, outCh: outCh, usageCh: usageCh, errCh: errCh}
		case usage, ok := <-usageCh:
			if ok {
				return DoneMsg{Usage: &usage}
			}
			return DoneMsg{}
		case err, ok := <-errCh:
			if ok && err != nil {
				return DoneMsg{Err: err}
			}
			return DoneMsg{}
		}
	}
}

// spinnerText returns an animated spinner character.
// tickCmd returns a command that sends a TickMsg after the spinner interval.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

func (m *Model) spinnerText() string {
	frames := []string{"⠋", "⠙", "⠚", "⠛", "⠜", "⠝", "⠞"}
	return frames[m.spinner%len(frames)]
}

// View implements tea.Model.
func (m Model) View() string {
	var sb strings.Builder

	// Header
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" go-Claw | %s ", m.rt.Model())))
	sb.WriteString(" ")

	// Vim mode indicator
	if m.vimMode {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("[VIM] "))
	}
	sb.WriteString("\n")

	// Messages area
	maxMsgLines := m.height - 8
	if maxMsgLines < 5 {
		maxMsgLines = 5
	}
	allLines := m.renderMessages()

	// Streaming content
	if m.state == StateStreaming || m.state == StateToolExec {
		streamText := m.streaming.GetText()
		if streamText != "" {
			renderer := NewTerminalRenderer()
			if m.markdownStream == nil {
				ms := NewMarkdownStreamState()
				m.markdownStream = &ms
			}
			m.markdownStream.Push(renderer, streamText)
			rendered := m.markdownStream.Flush(renderer)
			allLines = append(allLines, rendered)
		}
		thinking := m.streaming.GetThinking()
		if thinking != "" {
			allLines = append(allLines, lipgloss.NewStyle().Faint(true).Italic(true).Render("Thinking: "+truncate(thinking, 200)))
		}
		toolStatus := m.streaming.ToolStatusLine()
		if toolStatus != "" {
			allLines = append(allLines, toolStyle.Render(toolStatus))
		}
		allLines = append(allLines, m.spinnerText())
	}

	// Apply scroll offset and render visible lines
	if len(allLines) > maxMsgLines {
		start := len(allLines) - maxMsgLines - m.scrollOffset
		if start < 0 {
			start = 0
		}
		end := start + maxMsgLines
		if end > len(allLines) {
			end = len(allLines)
		}
		for _, l := range allLines[start:end] {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	} else {
		for _, l := range allLines {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}

	// Permission prompt
	if m.pendingPermission != nil {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(
			fmt.Sprintf("Permission required: %s", m.pendingPermission.ToolName)))
		sb.WriteString("\n  [y] Allow  [n] Deny\n")
	}

	// Status bar
	if m.statusLine != "" && m.state == StateIdle {
		sb.WriteString(m.statusLine)
		sb.WriteString("\n")
	}

	// Input area
	sb.WriteString("\n")
	sb.WriteString(m.input.View())
	sb.WriteString("\n")

	return sb.String()
}

func (m Model) renderMessages() []string {
	var lines []string
	w := m.width
	if w < 40 {
		w = 80
	}
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
			lines = append(lines, style.Render("> "+msg.Content))
		case "assistant":
			renderer := NewTerminalRenderer()
			lines = append(lines, renderer.RenderMarkdown(msg.Content))
		case "tool":
			if msg.Collapsed {
				lines = append(lines, toolStyle.Render("  "+msg.Content))
			} else {
				lines = append(lines, toolStyle.Render("  tool: "+msg.Content))
			}
		case "system":
			style := lipgloss.NewStyle().Faint(true).Italic(true).Foreground(lipgloss.Color("243"))
			lines = append(lines, style.Render("  "+msg.Content))
		case "thinking":
			style := lipgloss.NewStyle().Faint(true).Italic(true).Foreground(lipgloss.Color("99"))
			lines = append(lines, style.Render("  Thinking: "+msg.Content))
		}
	}
	return lines
}

func wordWrap(s string, width int) string {
	if width <= 0 {
		width = 80
	}
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			result = append(result, line)
			continue
		}
		for len(line) > width {
			space := strings.LastIndex(line[:width], " ")
			if space <= 0 {
				space = width
			}
			result = append(result, line[:space])
			line = line[space:]
			if len(line) > 0 && line[0] == ' ' {
				line = line[1:]
			}
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("15")).
	Background(lipgloss.Color("62")).
	Padding(0, 1)
