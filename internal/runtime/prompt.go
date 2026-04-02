package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
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

	// ---------- Environment ----------
	buf.WriteString("\n\n# Environment\n")
	fmt.Fprintf(&buf, " - Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		fmt.Fprintf(&buf, " - Working directory: %s\n", cwd)
	}
	if u, err := user.Current(); err == nil {
		fmt.Fprintf(&buf, " - User: %s\n", u.Username)
	}

	fmt.Fprintf(&buf, " - Current date: %s\n", time.Now().Format("2006-01-02"))

	// ---------- Platform details ----------
	writePlatformDetails(&buf)

	// ---------- Project structure ----------
	writeProjectContext(&buf, cwd)

	// ---------- Git context ----------
	writeGitContext(&buf)

	// ---------- Instruction files ----------
	writeInstructionFiles(&buf, cwd)

	return buf.String()
}

// ---------------------------------------------------------------------------
// Platform details
// ---------------------------------------------------------------------------

func writePlatformDetails(buf *strings.Builder) {
	shell := detectShell()
	if shell != "" {
		fmt.Fprintf(buf, " - Shell: %s\n", shell)
	}

	// Terminal capabilities (best-effort)
	term := os.Getenv("TERM")
	if term != "" {
		fmt.Fprintf(buf, " - TERM: %s\n", term)
	}
	colorterm := os.Getenv("COLORTERM")
	if colorterm != "" {
		fmt.Fprintf(buf, " - Color terminal: %s\n", colorterm)
	}
	if os.Getenv("NO_COLOR") != "" {
		buf.WriteString(" - NO_COLOR: set\n")
	}
}

// detectShell returns the user's likely shell, preferring $SHELL on Unix and
// falling back to ComSpec on Windows.
func detectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return filepath.Base(s)
	}
	if s := os.Getenv("ComSpec"); s != "" {
		return filepath.Base(s)
	}
	// pwsh detection
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "pwsh"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Project structure & type detection
// ---------------------------------------------------------------------------

func writeProjectContext(buf *strings.Builder, cwd string) {
	if cwd == "" {
		return
	}

	// Detect project type
	type detect struct {
		file   string
		label  string
		extract func(string) string // optional: extract info from file
	}
	detectors := []detect{
		{"go.mod", "Go", extractGoModule},
		{"package.json", "Node.js", extractNodeName},
		{"Cargo.toml", "Rust", nil},
		{"pyproject.toml", "Python (pyproject)", nil},
		{"setup.py", "Python (setup.py)", nil},
		{"requirements.txt", "Python (requirements)", nil},
		{"pom.xml", "Java (Maven)", nil},
		{"build.gradle", "Java (Gradle)", nil},
		{"build.gradle.kts", "Java (Gradle/Kotlin)", nil},
		{"Gemfile", "Ruby", nil},
		{"mix.exs", "Elixir", nil},
		{"composer.json", "PHP", nil},
		{"*.csproj", ".NET", nil},
		{"Makefile", "Make", nil},
		{"CMakeLists.txt", "CMake", nil},
	}

	var projectTypes []string
	for _, d := range detectors {
		matches, _ := filepath.Glob(filepath.Join(cwd, d.file))
		if len(matches) > 0 {
			entry := d.label
			if d.extract != nil {
				if info := d.extract(matches[0]); info != "" {
					entry = info
				}
			}
			projectTypes = append(projectTypes, entry)
		}
	}

	if len(projectTypes) > 0 {
		fmt.Fprintf(buf, " - Project type: %s\n", strings.Join(projectTypes, ", "))
	}

	// List top-level files and directories (max 30 entries)
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" && name != ".env.example" {
			continue // skip hidden files except notable ones
		}
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
		if len(names) >= 30 {
			break
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		fmt.Fprintf(buf, " - Top-level: %s\n", strings.Join(names, ", "))
	}
}

func extractGoModule(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return "Go module: " + strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return "Go"
}

func extractNodeName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `"name"`) {
			return "Node project: " + trimmed
		}
	}
	return "Node.js"
}

// ---------------------------------------------------------------------------
// Git context
// ---------------------------------------------------------------------------

func writeGitContext(buf *strings.Builder) {
	if !isGitRepo() {
		return
	}

	buf.WriteString("\n# Git status\n")

	// git status --short
	if out, err := gitOutput("status", "--short"); err == nil && out != "" {
		buf.WriteString(out)
	} else if out == "" {
		buf.WriteString("(working tree clean)\n")
	}

	// git diff --stat HEAD
	if out, err := gitOutput("diff", "--stat", "HEAD"); err == nil && out != "" {
		buf.WriteString("\nUncommitted changes:\n")
		buf.WriteString(out)
	}

	// git log --oneline -5
	if out, err := gitOutput("log", "--oneline", "-5"); err == nil && out != "" {
		buf.WriteString("\nRecent commits:\n")
		buf.WriteString(out)
	}

	// Current branch
	if out, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "" {
		fmt.Fprintf(buf, "Branch: %s\n", strings.TrimSpace(out))
	}
}

func isGitRepo() bool {
	if _, err := os.Stat(".git"); err == nil {
		return true
	}
	// Also check via git command for worktrees / submodules
	out, err := gitOutput("rev-parse", "--git-dir")
	return err == nil && out != ""
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir, _ = os.Getwd()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// Instruction files
// ---------------------------------------------------------------------------

func writeInstructionFiles(buf *strings.Builder, cwd string) {
	// Local project instruction files
	localFiles := []string{
		"CLAUDE.md",
		".claw/CLAUDE.md",
		".claw/instructions.md",
		"AGENTS.md",
	}
	for _, name := range localFiles {
		path := name
		if cwd != "" && !filepath.IsAbs(name) {
			path = filepath.Join(cwd, name)
		}
		if data, err := os.ReadFile(path); err == nil {
			fmt.Fprintf(buf, "\n\n# Project instructions (from %s)\n", name)
			buf.Write(data)
			break // use first found
		}
	}

	// Also load additional instruction files if the primary one was already found
	// Load .claw/instructions.md separately if it exists and wasn't the one loaded above
	for _, name := range []string{".claw/instructions.md", "AGENTS.md"} {
		path := name
		if cwd != "" && !filepath.IsAbs(name) {
			path = filepath.Join(cwd, name)
		}
		if data, err := os.ReadFile(path); err == nil {
			fmt.Fprintf(buf, "\n\n# Additional instructions (from %s)\n", name)
			buf.Write(data)
		}
	}

	// User-level instructions: ~/.claude/CLAUDE.md
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
			buf.WriteString("\n\n# User instructions (from ~/.claude/CLAUDE.md)\n")
			buf.Write(data)
		}
		// Also check ~/.claw/CLAUDE.md for claw-specific user instructions
		if data, err := os.ReadFile(filepath.Join(home, ".claw", "CLAUDE.md")); err == nil {
			buf.WriteString("\n\n# User instructions (from ~/.claw/CLAUDE.md)\n")
			buf.Write(data)
		}
	}
}

