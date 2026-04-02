package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-claw/claw/internal/tools"
)

// Version is set at build time via -ldflags.
var Version = "0.4.0"

// debugToolCallEnabled tracks the debug-tool-call toggle state.
var debugToolCallEnabled bool

// CommandSpec describes a slash command.
type CommandSpec struct {
	Name           string
	Aliases        []string
	Summary        string
	Args           string // argument hint
	ResumeMode     bool   // works in resume mode
	Handler        func(args string) (string, error)
}

// Commands returns all built-in slash commands.
func Commands() []CommandSpec {
	return []CommandSpec{
		{
			Name:       "help",
			Summary:    "Show help for slash commands",
			ResumeMode: true,
			Handler:    cmdHelp,
		},
		{
			Name:       "status",
			Summary:    "Show session status",
			ResumeMode: true,
			Handler:    cmdStatus,
		},
		{
			Name:       "compact",
			Summary:    "Compact conversation history",
			ResumeMode: true,
			Handler:    cmdCompact,
		},
		{
			Name:       "model",
			Summary:    "Show or change current model",
			Args:       "[model]",
			Handler:    cmdModel,
		},
		{
			Name:       "permissions",
			Summary:    "Show or change permission mode",
			Args:       "[read-only|workspace-write|danger-full-access]",
			Handler:    cmdPermissions,
		},
		{
			Name:       "clear",
			Summary:    "Clear conversation history",
			Args:       "[--confirm]",
			ResumeMode: true,
			Handler:    cmdClear,
		},
		{
			Name:       "cost",
			Summary:    "Show token usage and estimated cost",
			ResumeMode: true,
			Handler:    cmdCost,
		},
		{
			Name:       "resume",
			Summary:    "Resume a previous session",
			Args:       "<session-path>",
			Handler:    cmdResume,
		},
		{
			Name:       "config",
			Summary:    "Show or edit configuration",
			Args:       "[env|hooks|model|plugins]",
			ResumeMode: true,
			Handler:    cmdConfig,
		},
		{
			Name:       "memory",
			Summary:    "Show or manage memory",
			ResumeMode: true,
			Handler:    cmdMemory,
		},
		{
			Name:       "init",
			Summary:    "Initialize project configuration",
			Handler:    cmdInit,
		},
		{
			Name:       "diff",
			Summary:    "Show uncommitted changes",
			Handler:    cmdDiff,
		},
		{
			Name:       "version",
			Summary:    "Show version info",
			ResumeMode: true,
			Handler:    cmdVersion,
		},
		{
			Name:    "commit",
			Summary: "Generate a commit message and create a git commit",
			Handler: cmdCommit,
		},
		{
			Name:    "pr",
			Summary: "Draft or create a pull request",
			Args:    "[context]",
			Handler: cmdPR,
		},
		{
			Name:    "issue",
			Summary: "Draft or create a GitHub issue",
			Args:    "[context]",
			Handler: cmdIssue,
		},
		{
			Name:       "export",
			Summary:    "Export conversation to file",
			Args:       "[file]",
			ResumeMode: true,
			Handler:    cmdExport,
		},
		{
			Name:       "session",
			Summary:    "Manage sessions",
			Args:       "[list|switch <id>]",
			Handler:    cmdSession,
		},
		{
			Name:       "agents",
			Summary:    "List and manage agents",
			ResumeMode: true,
			Handler:    cmdAgents,
		},
		{
			Name:       "skills",
			Summary:    "List and manage skills",
			ResumeMode: true,
			Handler:    cmdSkills,
		},
		{
			Name:       "branch",
			Summary:    "Git branch operations",
			Args:       "[list|create <name>|switch <name>]",
			Handler:    cmdBranch,
		},
		{
			Name:       "worktree",
			Summary:    "Git worktree operations",
			Args:       "[list|add <path>|remove <path>]",
			Handler:    cmdWorktree,
		},
		{
			Name:       "todo",
			Summary:    "Show current todo list",
			ResumeMode: true,
			Handler:    cmdTodo,
		},
		{
			Name:       "plugin",
			Aliases:    []string{"plugins", "marketplace"},
			Summary:    "Manage plugins",
			Args:       "[list|install|enable|disable|uninstall]",
			Handler:    cmdPlugin,
		},
		{
			Name:       "debug-tool-call",
			Summary:    "Toggle tool call debugging",
			ResumeMode: true,
			Handler:    cmdDebugToolCall,
		},
		{
			Name:    "bughunter",
			Summary: "Inspect the codebase for likely bugs",
			Args:    "[scope]",
			Handler: cmdBughunter,
		},
		{
			Name:    "commit-push-pr",
			Summary: "Commit, push, and create a PR",
			Args:    "[context]",
			Handler: cmdCommitPushPR,
		},
		{
			Name:    "ultraplan",
			Summary: "Deep planning with multi-step reasoning",
			Args:    "[task]",
			Handler: cmdUltraplan,
		},
		{
			Name:    "teleport",
			Summary: "Jump to a file or symbol by searching the workspace",
			Args:    "<query>",
			Handler: cmdTeleport,
		},
	}
}

// Parse parses a slash command line and returns the matching command and args.
func Parse(line string) (*CommandSpec, string) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return nil, ""
	}
	line = line[1:]
	parts := strings.SplitN(line, " ", 2)
	name := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	for i := range Commands() {
		cmd := &Commands()[i]
		if cmd.Name == name {
			return cmd, args
		}
		for _, alias := range cmd.Aliases {
			if alias == name {
				return cmd, args
			}
		}
	}
	return nil, name
}

// --- Command Handlers ---

func cmdHelp(args string) (string, error) {
	var buf strings.Builder
	buf.WriteString("Slash commands:\n")
	for _, cmd := range Commands() {
		name := "/" + cmd.Name
		if len(cmd.Aliases) > 0 {
			name += " (" + strings.Join(cmd.Aliases, ", ") + ")"
		}
		if cmd.Args != "" {
			name += " " + cmd.Args
		}
		fmt.Fprintf(&buf, "  %-30s %s\n", name, cmd.Summary)
	}
	return buf.String(), nil
}

func cmdStatus(args string) (string, error) {
	var buf strings.Builder
	fmt.Fprintf(&buf, "Claw Code (Go) v%s\n", Version)
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&buf, "  Working dir: %s\n", cwd)
	}
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  Branch: %s\n", strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(&buf, "  Agents: %d running\n", len(tools.GetAgents()))
	fmt.Fprintf(&buf, "  Todos: %d\n", len(tools.GetTodos()))
	if debugToolCallEnabled {
		buf.WriteString("  Debug tool calls: ON\n")
	}
	return buf.String(), nil
}

func cmdCompact(args string) (string, error) {
	return "Compaction triggered.", nil
}

func cmdModel(args string) (string, error) {
	if args != "" {
		return fmt.Sprintf("Model set to: %s", args), nil
	}
	current := os.Getenv("ANTHROPIC_MODEL")
	if current == "" {
		current = "(default)"
	}
	return fmt.Sprintf("Current model: %s\nUse /model <name> to change", current), nil
}

func cmdPermissions(args string) (string, error) {
	validModes := map[string]bool{"read-only": true, "workspace-write": true, "danger-full-access": true}
	if args != "" {
		if !validModes[args] {
			return "", fmt.Errorf("invalid mode %q; valid: read-only, workspace-write, danger-full-access", args)
		}
		return fmt.Sprintf("Permission mode set to: %s", args), nil
	}
	current := os.Getenv("CLAW_PERMISSION_MODE")
	if current == "" {
		current = "danger-full-access"
	}
	return fmt.Sprintf("Current mode: %s", current), nil
}

func cmdClear(args string) (string, error) {
	return "Session cleared.", nil
}

func cmdCost(args string) (string, error) {
	return "Cost tracking: use /status to see session info. Per-token cost depends on model.", nil
}

func cmdResume(args string) (string, error) {
	if args == "" {
		return "", fmt.Errorf("usage: /resume <session-path>")
	}
	if _, err := os.Stat(args); err != nil {
		return "", fmt.Errorf("session file not found: %s", args)
	}
	return fmt.Sprintf("Resuming session: %s", args), nil
}

func cmdConfig(args string) (string, error) {
	switch args {
	case "env":
		return fmt.Sprintf("ANTHROPIC_BASE_URL=%s\nANTHROPIC_API_KEY=%s\nCLAW_PERMISSION_MODE=%s",
			os.Getenv("ANTHROPIC_BASE_URL"),
			maskKey(os.Getenv("ANTHROPIC_API_KEY")),
			os.Getenv("CLAW_PERMISSION_MODE")), nil
	case "hooks":
		return "Hooks are configured in .claw/settings.json under \"hooks\".\nUse /config to view current settings.", nil
	case "model":
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = "(default: claude-sonnet-4-6)"
		}
		return fmt.Sprintf("Model: %s", model), nil
	case "plugins":
		return "No plugins installed.", nil
	default:
		return "Usage: /config [env|hooks|model|plugins]", nil
	}
}

func cmdMemory(args string) (string, error) {
	home, _ := os.UserHomeDir()
	var sections []string

	// Check project memory
	for _, dir := range []string{".claude", ".claw"} {
		memDir := filepath.Join(dir, "memory")
		if entries, err := os.ReadDir(memDir); err == nil && len(entries) > 0 {
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
			}
			sections = append(sections, fmt.Sprintf("Project memory (%s):\n  %s", dir, strings.Join(files, "\n  ")))
		}
	}

	// Check user memory
	for _, dir := range []string{".claude", ".claw"} {
		memDir := filepath.Join(home, dir, "memory")
		if entries, err := os.ReadDir(memDir); err == nil && len(entries) > 0 {
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
			}
			sections = append(sections, fmt.Sprintf("User memory (~/%s):\n  %s", dir, strings.Join(files, "\n  ")))
		}
	}

	if len(sections) == 0 {
		return "No memory files found.", nil
	}
	return strings.Join(sections, "\n\n"), nil
}

func cmdInit(args string) (string, error) {
	// Create .claw directory if needed
	if err := os.MkdirAll(".claw", 0755); err != nil {
		return "", err
	}

	created := []string{}

	// Create settings.json if not exists
	settingsFile := ".claw/settings.json"
	if _, err := os.Stat(settingsFile); err != nil {
		content := `{
  "model": "",
  "permissionMode": "danger-full-access",
  "hooks": {
    "PreToolUse": [],
    "PostToolUse": []
  }
}`
		if err := os.WriteFile(settingsFile, []byte(content), 0644); err != nil {
			return "", err
		}
		created = append(created, settingsFile)
	}

	// Create CLAUDE.md if not exists
	clawMD := "CLAUDE.md"
	if _, err := os.Stat(clawMD); err != nil {
		content := "# Project Instructions\n\nAdd project-specific instructions here.\n"
		if err := os.WriteFile(clawMD, []byte(content), 0644); err != nil {
			return "", err
		}
		created = append(created, clawMD)
	}

	if len(created) == 0 {
		return "Project already initialized. All files exist.", nil
	}
	return "Created:\n  " + strings.Join(created, "\n  "), nil
}

func cmdDiff(args string) (string, error) {
	// Show staged and unstaged changes
	var buf strings.Builder

	if out, err := exec.Command("git", "diff", "--stat").CombinedOutput(); err == nil && len(out) > 0 {
		buf.WriteString("Unstaged changes:\n")
		buf.WriteString(string(out))
	}
	if out, err := exec.Command("git", "diff", "--cached", "--stat").CombinedOutput(); err == nil && len(out) > 0 {
		buf.WriteString("Staged changes:\n")
		buf.WriteString(string(out))
	}
	if buf.Len() == 0 {
		return "No uncommitted changes.", nil
	}
	return buf.String(), nil
}

func cmdVersion(args string) (string, error) {
	return fmt.Sprintf("Claw Code (Go) v%s", Version), nil
}

func cmdCommit(args string) (string, error) {
	// Get a summary of changes for the commit message
	changes, _ := exec.Command("git", "diff", "--stat", "HEAD").CombinedOutput()
	statusOut, _ := exec.Command("git", "status", "--short").CombinedOutput()

	// Build commit message
	commitMsg := "claw: automated commit"
	if args != "" {
		commitMsg = args
	} else if len(statusOut) > 0 {
		// Generate from status
		lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
		if len(lines) > 0 {
			commitMsg = fmt.Sprintf("claw: update %d file(s)", len(lines))
		}
	}

	// Show what will be committed
	var buf strings.Builder
	if len(changes) > 0 {
		buf.WriteString("Changes to commit:\n")
		buf.WriteString(string(changes))
		buf.WriteString("\n")
	}

	// Stage all changes
	exec.Command("git", "add", "-A").Run()

	// Create commit
	cmd := exec.Command("git", "commit", "-m", commitMsg)
	out, err := cmd.CombinedOutput()
	buf.Write(out)
	if err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func cmdPR(args string) (string, error) {
	// Check if gh is available
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found. Install: https://cli.github.com")
	}

	// Check for uncommitted changes first
	if out, _ := exec.Command("git", "status", "--porcelain").CombinedOutput(); len(out) > 0 {
		return "You have uncommitted changes. Commit or stash them first.", nil
	}

	cmd := exec.Command("gh", "pr", "create", "--fill")
	if args != "" {
		cmd = exec.Command("gh", "pr", "create", "--fill", "--body", args)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cmdIssue(args string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found. Install: https://cli.github.com")
	}

	title := "New issue"
	if args != "" {
		title = args
	}
	cmd := exec.Command("gh", "issue", "create", "--title", title)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cmdExport(args string) (string, error) {
	filename := args
	if filename == "" {
		filename = fmt.Sprintf("claw-export-%s.md", time.Now().Format("20060102-150405"))
	}

	// Try to find the latest session
	sessionDir := ".claw-sessions"
	data, err := findLatestSession(sessionDir)
	if err != nil {
		// Export a placeholder
		content := fmt.Sprintf("# Claw Code Export\n\nExported: %s\n\nNo session data found.\n", time.Now().Format(time.RFC3339))
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Exported to: %s (no session data)", filename), nil
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported to: %s", filename), nil
}

func cmdSession(args string) (string, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 || parts[0] == "list" {
		entries, err := os.ReadDir(".claw-sessions")
		if err != nil {
			return "No saved sessions.", nil
		}
		if len(entries) == 0 {
			return "No saved sessions.", nil
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return "Sessions:\n  " + strings.Join(names, "\n  "), nil
	}
	if parts[0] == "switch" && len(parts) >= 2 {
		return fmt.Sprintf("Switch to session: %s (not yet connected to runtime)", parts[1]), nil
	}
	return "Usage: /session [list|switch <id>]", nil
}

func cmdAgents(args string) (string, error) {
	agents := tools.GetAgents()
	if len(agents) == 0 {
		return "No agents running.", nil
	}
	var buf strings.Builder
	for _, a := range agents {
		status := a.Status
		if a.Status == "running" {
			elapsed := time.Since(a.StartedAt).Truncate(time.Second)
			status = fmt.Sprintf("running (%s)", elapsed)
		}
		fmt.Fprintf(&buf, "  %s [%s] %s - %s\n", a.ID, status, a.Type, a.Description)
	}
	return buf.String(), nil
}

func cmdSkills(args string) (string, error) {
	skills := tools.DiscoverSkills()
	if len(skills) == 0 {
		return "No skills found. Create skills in .claw/skills/ or ~/.claw/skills/", nil
	}
	return "Available skills:\n  " + strings.Join(skills, "\n  "), nil
}

func cmdBranch(args string) (string, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		out, err := exec.Command("git", "branch", "-a").CombinedOutput()
		return string(out), err
	}
	switch parts[0] {
	case "create":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /branch create <name>")
		}
		out, err := exec.Command("git", "checkout", "-b", parts[1]).CombinedOutput()
		return string(out), err
	case "switch":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /branch switch <name>")
		}
		out, err := exec.Command("git", "switch", parts[1]).CombinedOutput()
		return string(out), err
	default:
		return "", fmt.Errorf("usage: /branch [list|create <name>|switch <name>]")
	}
}

func cmdWorktree(args string) (string, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		out, err := exec.Command("git", "worktree", "list").CombinedOutput()
		return string(out), err
	}
	switch parts[0] {
	case "add":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /worktree add <path>")
		}
		out, err := exec.Command("git", "worktree", "add", parts[1]).CombinedOutput()
		return string(out), err
	case "remove":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: /worktree remove <path>")
		}
		out, err := exec.Command("git", "worktree", "remove", parts[1]).CombinedOutput()
		return string(out), err
	case "prune":
		out, err := exec.Command("git", "worktree", "prune").CombinedOutput()
		return string(out), err
	default:
		return "", fmt.Errorf("usage: /worktree [list|add <path>|remove <path>|prune]")
	}
}

func cmdTodo(args string) (string, error) {
	todos := tools.GetTodos()
	if len(todos) == 0 {
		return "No todos set. Use the TodoWrite tool to create tasks.", nil
	}
	var buf strings.Builder
	for _, t := range todos {
		icon := "[ ]"
		switch t.Status {
		case "in_progress":
			icon = "[~]"
		case "completed":
			icon = "[x]"
		}
		fmt.Fprintf(&buf, "  %s %s\n", icon, t.Content)
	}
	return buf.String(), nil
}

func cmdPlugin(args string) (string, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 || parts[0] == "list" {
		return "No plugins installed. Place plugins in .claw/plugins/ or ~/.claw/plugins/", nil
	}
	return fmt.Sprintf("Plugin operation '%s' not yet implemented.", parts[0]), nil
}

func cmdDebugToolCall(args string) (string, error) {
	debugToolCallEnabled = !debugToolCallEnabled
	state := "OFF"
	if debugToolCallEnabled {
		state = "ON"
	}
	return fmt.Sprintf("Tool call debugging: %s", state), nil
}

func cmdBughunter(args string) (string, error) {
	scope := args
	if scope == "" {
		scope = "."
	}
	// Return a prompt that the agent runtime can pick up
	return fmt.Sprintf("Bughunter: inspecting %s for likely bugs.\nUse tools to read files, search for patterns, and identify potential issues.", scope), nil
}

func cmdCommitPushPR(args string) (string, error) {
	var buf strings.Builder

	// Step 1: Commit
	commitMsg := "claw: automated commit"
	if args != "" {
		commitMsg = args
	}

	// Check for changes
	statusOut, _ := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if len(statusOut) == 0 {
		return "No changes to commit.", nil
	}

	exec.Command("git", "add", "-A").Run()
	out, err := exec.Command("git", "commit", "-m", commitMsg).CombinedOutput()
	buf.WriteString("1. Commit:\n")
	buf.WriteString(string(out))
	if err != nil {
		return buf.String(), err
	}

	// Step 2: Push
	branch, _ := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	branchName := strings.TrimSpace(string(branch))

	out, err = exec.Command("git", "push", "-u", "origin", branchName).CombinedOutput()
	buf.WriteString("\n2. Push:\n")
	buf.WriteString(string(out))
	if err != nil {
		buf.WriteString("(push may have failed — branch may already be up to date)\n")
	}

	// Step 3: Create PR
	if _, err := exec.LookPath("gh"); err != nil {
		buf.WriteString("\n3. PR: gh CLI not found, skipping PR creation.\n")
		return buf.String(), nil
	}

	out, err = exec.Command("gh", "pr", "create", "--fill").CombinedOutput()
	buf.WriteString("\n3. PR:\n")
	buf.WriteString(string(out))
	if err != nil {
		return buf.String(), err
	}

	return buf.String(), nil
}

func cmdUltraplan(args string) (string, error) {
	task := args
	if task == "" {
		return "", fmt.Errorf("usage: /ultraplan <task description>")
	}
	return fmt.Sprintf("Ultraplan: deep planning for: %s\n(This command works best when connected to an agent runtime)", task), nil
}

func cmdTeleport(args string) (string, error) {
	if args == "" {
		return "", fmt.Errorf("usage: /teleport <file-or-symbol>")
	}

	// Try to find files matching the query
	var results []string

	// First try exact file match
	if _, err := os.Stat(args); err == nil {
		results = append(results, args+" (file)")
	}

	// Try git grep for symbols
	if out, err := exec.Command("git", "grep", "-n", "--max-count", "10", args).CombinedOutput(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			for _, l := range lines {
				if len(results) >= 10 {
					break
				}
				parts := strings.SplitN(l, ":", 3)
				if len(parts) >= 2 {
					results = append(results, parts[0]+":"+parts[1])
				}
			}
		}
	}

	// Try find for filenames
	if out, err := exec.Command("find", ".", "-name", "*"+args+"*", "-type", "f").CombinedOutput(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f != "" && len(results) < 10 {
				results = append(results, f)
			}
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No matches found for: %s", args), nil
	}
	return "Found:\n  " + strings.Join(results, "\n  "), nil
}

// --- Helpers ---

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func findLatestSession(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("no sessions")
	}
	// Sort by modification time, pick latest
	var latest string
	var latestTime time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(dir, e.Name())
		}
	}
	if latest == "" {
		return nil, fmt.Errorf("no sessions")
	}
	return os.ReadFile(latest)
}

// IsDebugEnabled returns whether tool call debugging is enabled.
func IsDebugEnabled() bool {
	return debugToolCallEnabled
}
