package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

	headerStyleMarkdown = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39"))

	linkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Underline(true)

	quoteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	listBulletStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("180"))

	codeBlockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	inlineCodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("180")).
			Background(lipgloss.Color("236"))

	boldStyle = lipgloss.NewStyle().Bold(true)
)

// Markdown patterns
var (
	boldPattern      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicPattern    = regexp.MustCompile(`(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)`)
	inlineCodePattern = regexp.MustCompile("`(.*?)`")
	linkPattern      = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
	headerPattern    = regexp.MustCompile(`^(#{1,6})\s+(.+)`)
	quotePattern     = regexp.MustCompile(`^>\s?(.+)`)
	ulPattern        = regexp.MustCompile(`^[\-\*]\s+(.+)`)
	olPattern        = regexp.MustCompile(`^\d+\.\s+(.+)`)
	hrPattern        = regexp.MustCompile(`^---+$`)
)

// RenderMessage renders a message with appropriate styling.
func RenderMessage(msg Message, width int) string {
	switch msg.Role {
	case "user":
		return userStyle.Render("> " + msg.Content)
	case "assistant":
		return renderAssistant(msg.Content, width)
	case "tool":
		return toolStyle.Render("  tool: " + msg.Content)
	case "system":
		return systemStyle.Render("  " + msg.Content)
	default:
		return msg.Content
	}
}

func renderAssistant(content string, width int) string {
	var lines []string
	inCodeBlock := false
	var codeBlockLines []string
	var codeLang string

	for _, line := range strings.Split(content, "\n") {
		// Handle code blocks
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCodeBlock {
				// End code block
				codeContent := strings.Join(codeBlockLines, "\n")
				lang := ""
				if codeLang != "" {
					lang = codeLang + " "
				}
				rendered := codeBlockStyle.Render(lang + codeContent)
				lines = append(lines, rendered)
				inCodeBlock = false
				codeBlockLines = nil
				codeLang = ""
			} else {
				// Start code block
				inCodeBlock = true
				codeLang = strings.TrimPrefix(strings.TrimSpace(line), "```")
			}
			continue
		}

		if inCodeBlock {
			codeBlockLines = append(codeBlockLines, line)
			continue
		}

		// Process non-code lines
		line = renderMarkdownLine(line)
		lines = append(lines, line)
	}

	return assistantStyle.Render(strings.Join(lines, "\n"))
}

func renderMarkdownLine(line string) string {
	// Horizontal rule
	if hrPattern.MatchString(strings.TrimSpace(line)) {
		return dividerStyle.Render(strings.Repeat("-", 40))
	}

	// Headers
	if m := headerPattern.FindStringSubmatch(line); len(m) >= 3 {
		level := len(m[1])
		text := renderInline(m[2])
		switch level {
		case 1:
			return headerStyleMarkdown.Render(text)
		case 2:
			return headerStyleMarkdown.Bold(true).Render(text)
		default:
			return boldStyle.Render(text)
		}
	}

	// Blockquotes
	if m := quotePattern.FindStringSubmatch(line); len(m) >= 2 {
		return quoteStyle.Render("| " + renderInline(m[1]))
	}

	// Unordered lists
	if m := ulPattern.FindStringSubmatch(line); len(m) >= 2 {
		return listBulletStyle.Render("  * ") + renderInline(m[1])
	}

	// Ordered lists
	if m := olPattern.FindStringSubmatch(line); len(m) >= 2 {
		return listBulletStyle.Render("  1. ") + renderInline(m[1])
	}

	return renderInline(line)
}

func renderInline(s string) string {
	// Bold: **text**
	s = boldPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := boldPattern.FindStringSubmatch(match)[1]
		return boldStyle.Render(inner)
	})

	// Links: [text](url)
	s = linkPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := linkPattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			return linkStyle.Render(parts[1]) + " (" + parts[2] + ")"
		}
		return match
	})

	// Inline code: `text`
	s = inlineCodePattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := inlineCodePattern.FindStringSubmatch(match)
		if len(inner) >= 2 {
			return inlineCodeStyle.Render(inner[1])
		}
		return match
	})

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
