package runtime

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultSystemPrompt returns the default system prompt with environment info.
func DefaultSystemPrompt() string {
	var buf strings.Builder

	buf.WriteString(`You are Claw Code, an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming.

# System
 - All text you output outside of tool use is displayed to the user.
 - Tools are executed in a user-selected permission mode.
 - Tool results and user messages may include system-reminder tags carrying system information.

# Doing tasks
 - Read relevant code before changing it and keep changes tightly scoped to the request.
 - Do not add speculative abstractions, compatibility shims, or unrelated cleanup.
 - Do not create files unless they are required to complete the task.
 - If an approach fails, diagnose the failure before switching tactics.
 - Be careful not to introduce security vulnerabilities.

# Executing actions with care
Carefully consider reversibility and blast radius. Local, reversible actions are usually fine. Actions that affect shared systems should be explicitly authorized.

# Tools available
You have access to the following tools:
 - bash: Execute shell commands
 - read_file: Read file contents
 - write_file: Write content to files
 - edit_file: Replace strings in files
 - glob: Find files matching a pattern
 - grep: Search file contents with regex
 - WebFetch: Fetch and read web pages
 - WebSearch: Search the web via DuckDuckGo
 - TodoWrite: Track task progress
 - Agent: Launch sub-agents for complex tasks
 - Skill: Execute named skills
 - NotebookEdit: Edit Jupyter notebooks
 - Sleep: Wait for a duration
 - ToolSearch: Search available tools

# Output format
 - Keep responses concise and to the point.
 - Use markdown formatting when helpful.
 - When referencing files, include the file path.`)

	// Add environment context
	buf.WriteString("\n\n# Environment\n")
	fmt.Fprintf(&buf, " - Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&buf, " - Working directory: %s\n", cwd)
	}
	if u, err := user.Current(); err == nil {
		fmt.Fprintf(&buf, " - User: %s\n", u.Username)
	}

	// Check for project context
	if data, err := os.ReadFile("go.mod"); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 {
			fmt.Fprintf(&buf, " - Go module: %s\n", strings.TrimSpace(lines[0]))
		}
	}
	if data, err := os.ReadFile("package.json"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, l := range lines {
			if strings.Contains(l, `"name"`) {
				fmt.Fprintf(&buf, " - Node project: %s\n", strings.TrimSpace(l))
				break
			}
		}
	}

	// Check for git repo
	if _, err := os.Stat(".git"); err == nil {
		fmt.Fprintf(&buf, " - Git repository: yes\n")
	}

	fmt.Fprintf(&buf, " - Current date: %s\n", time.Now().Format("2006-01-02"))

	// Load CLAUDE.md if present
	for _, name := range []string{"CLAUDE.md", ".claw/CLAUDE.md"} {
		if data, err := os.ReadFile(name); err == nil {
			buf.WriteString("\n\n# Project instructions (from " + name + ")\n")
			buf.WriteString(string(data))
			break
		}
	}

	// Load ~/.claude/CLAUDE.md
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
			buf.WriteString("\n\n# User instructions (from ~/.claude/CLAUDE.md)\n")
			buf.WriteString(string(data))
		}
	}

	return buf.String()
}

// ConversationRuntime methods

func (rt *ConversationRuntime) Model() string {
	return rt.model
}

func (rt *ConversationRuntime) MessageCount() int {
	return len(rt.session.Messages)
}

func (rt *ConversationRuntime) Clear() {
	rt.session = NewSession()
}
