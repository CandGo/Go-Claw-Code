package tui

import (
	"context"
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

// StreamMsg carries streaming text from the API.
type StreamMsg struct {
	Text string
}

// ToolMsg carries tool execution updates.
type ToolMsg struct {
	Name   string
	Result string
	Err    error
	Done   bool
}

// DoneMsg signals the turn is complete.
type DoneMsg struct {
	Usage *runtime.TokenUsage
	Err   error
}

// Model is the Bubbletea model for the Claw TUI.
type Model struct {
	state       SessionState
	rt          *runtime.ConversationRuntime
	compaction  runtime.CompactionConfig
	input       textarea.Model
	messages    []Message
	statusLine  string
	width       int
	height      int
	err         error
	ctx         context.Context
	cancel      context.CancelFunc
}

// Message represents one displayed message in the conversation.
type Message struct {
	Role    string // "user", "assistant", "tool", "system"
	Content string
	Time    time.Time
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
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 4)
		return m, nil

	case tea.KeyMsg:
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

		// Enter to submit (only in idle state)
		if m.state == StateIdle {
			switch msg.String() {
			case "enter":
				if !m.input.Focused() {
					break
				}
				text := strings.TrimSpace(m.input.Value())
				if text == "" {
					return m, nil
				}
				m.input.Reset()

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
						m.rt.Compact(m.compaction)
						m.messages = append(m.messages, Message{Role: "system", Content: "Compacted.", Time: time.Now()})
					}
					if cmd.Name == "clear" {
						m.rt.Clear()
						m.messages = nil
						m.messages = append(m.messages, Message{Role: "system", Content: "Session cleared.", Time: time.Now()})
					}
					return m, nil
				}

				// Submit user message
				m.messages = append(m.messages, Message{Role: "user", Content: text, Time: time.Now()})
				m.state = StateStreaming
				m.statusLine = "Thinking..."
				m.ctx, m.cancel = context.WithCancel(context.Background())
				return m, tea.Batch(m.runTurn(text))

			case "ctrl+l":
				// Clear screen
				m.messages = nil
				return m, tea.ClearScreen
			}
		}

	case StreamMsg:
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content += msg.Text
		} else {
			m.messages = append(m.messages, Message{Role: "assistant", Content: msg.Text, Time: time.Now()})
		}
		return m, nil

	case ToolMsg:
		m.statusLine = "Tool: " + msg.Name
		if msg.Done {
			m.state = StateIdle
		}
		return m, nil

	case DoneMsg:
		m.state = StateIdle
		if msg.Err != nil {
			m.messages = append(m.messages, Message{Role: "system", Content: "error: " + msg.Err.Error(), Time: time.Now()})
		}
		if msg.Usage != nil {
			m.statusLine = lipgloss.NewStyle().Faint(true).Render(
				sprintf("tokens: in=%d out=%d", msg.Usage.InputTokens, msg.Usage.OutputTokens))
		} else {
			m.statusLine = ""
		}
		return m, nil
	}

	// Update input area
	newInput, cmd := m.input.Update(msg)
	m.input = newInput
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m Model) View() string {
	var sb strings.Builder

	// Header
	sb.WriteString(headerStyle.Render(" Claw Code v0.3.0 "))
	sb.WriteString("  ")
	sb.WriteString(lipgloss.NewStyle().Faint(true).Render("Model: " + m.rt.Model()))
	sb.WriteString("\n")

	// Messages area
	maxMsgLines := m.height - 8
	if maxMsgLines < 5 {
		maxMsgLines = 5
	}
	lines := m.renderMessages()
	if len(lines) > maxMsgLines {
		lines = lines[len(lines)-maxMsgLines:]
	}
	for _, l := range lines {
		sb.WriteString(l + "\n")
	}

	// Status
	if m.statusLine != "" {
		sb.WriteString(m.statusLine + "\n")
	}

	// Input area
	sb.WriteString("\n")
	sb.WriteString(m.input.View())
	sb.WriteString("\n")

	return sb.String()
}

func (m Model) renderMessages() []string {
	var lines []string
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
			lines = append(lines, style.Render("> "+msg.Content))
		case "assistant":
			lines = append(lines, wordWrap(msg.Content, m.width-2))
		case "tool":
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
			lines = append(lines, style.Render("  tool: "+msg.Content))
		case "system":
			style := lipgloss.NewStyle().Faint(true).Italic(true)
			lines = append(lines, style.Render("  "+msg.Content))
		}
	}
	return lines
}

func (m Model) runTurn(prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
		defer cancel()

		outputs, usage, err := m.rt.RunTurn(ctx, prompt)

		// Collect assistant text from outputs
		var assistantText strings.Builder
		for _, out := range outputs {
			switch out.Type {
			case "text":
				assistantText.WriteString(out.Text)
				assistantText.WriteString("\n")
			case "tool_use":
				_ = out.ToolName
			}
		}

		return DoneMsg{Usage: usage, Err: err}
	}
}

func sprintf(format string, args ...interface{}) string {
	return format
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
			// Find last space before width
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

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("15")).
	Background(lipgloss.Color("62")).
	Padding(0, 1)
