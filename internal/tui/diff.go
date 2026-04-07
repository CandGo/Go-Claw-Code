package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderDiff renders a unified diff with ANSI color coding.
// Removed lines (prefixed with -) are shown in red, added lines (prefixed with +) in green,
// and context/header lines are shown in dim gray.
func RenderDiff(diff string) string {
	if diff == "" {
		return ""
	}

	var lines []string
	for _, line := range strings.Split(diff, "\n") {
		if len(line) == 0 {
			lines = append(lines, "")
			continue
		}

		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(line))
		case strings.HasPrefix(line, "@@"):
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Render(line))
		case strings.HasPrefix(line, "-"):
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(line))
		case strings.HasPrefix(line, "+"):
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render(line))
		default:
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("249")).Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

// RenderDiffStats renders diff stat output with file names and change counts.
func RenderDiffStats(stat string) string {
	if stat == "" {
		return ""
	}

	var lines []string
	for _, line := range strings.Split(stat, "\n") {
		if strings.Contains(line, "|") {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 {
				file := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(strings.TrimSpace(parts[0]))
				changes := lipgloss.NewStyle().Foreground(lipgloss.Color("249")).Render("|" + parts[1])
				lines = append(lines, file+" "+changes)
			} else {
				lines = append(lines, line)
			}
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(line))
		}
	}
	return strings.Join(lines, "\n")
}
