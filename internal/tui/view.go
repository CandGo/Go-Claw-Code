package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles for different message types.
var (
	userStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	systemStyle = lipgloss.NewStyle().
			Faint(true).
			Italic(true).
			Foreground(lipgloss.Color("243"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)
)

// RenderMessage renders a message with appropriate styling.
func RenderMessage(msg Message, width int) string {
	switch msg.Role {
	case "user":
		return userStyle.Render("▸ " + msg.Content)
	case "assistant":
		return renderAssistant(msg.Content, width)
	case "tool":
		return toolStyle.Render("  ⚙ " + msg.Content)
	case "system":
		return systemStyle.Render("  " + msg.Content)
	default:
		return msg.Content
	}
}

func renderAssistant(content string, width int) string {
	// Simple markdown-like rendering
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		// Bold: **text**
		line = renderBold(line)
		// Code blocks: ```...```
		line = renderCode(line)
		lines = append(lines, line)
	}
	return assistantStyle.Render(strings.Join(lines, "\n"))
}

func renderBold(s string) string {
	// Simple **bold** rendering
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		bold := lipgloss.NewStyle().Bold(true).Render(s[start+2 : start+2+end])
		s = s[:start] + bold + s[start+2+end+2:]
	}
	return s
}

func renderCode(s string) string {
	if strings.HasPrefix(s, "```") {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Render(s)
	}
	// Inline code: `text`
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		code := lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Background(lipgloss.Color("236")).Render(s[start+1 : start+1+end])
		s = s[:start] + code + s[start+1+end+1:]
	}
	return s
}

// RenderStatusBar renders the bottom status bar.
func RenderStatusBar(model, status string, width int) string {
	left := " " + model
	right := status + " "
	content := left + strings.Repeat(" ", max(width-len(left)-len(right), 0)) + right
	return statusBarStyle.Render(content)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
