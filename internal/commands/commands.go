package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	clawauth "github.com/CandGo/Go-Claw-Code/internal/auth"
	"github.com/CandGo/Go-Claw-Code/internal/plugins"
	"github.com/CandGo/Go-Claw-Code/internal/runtime"
	"github.com/CandGo/Go-Claw-Code/internal/tools"
)

// Version is set at build time via -ldflags.
var Version = "0.4.0"

// debugToolCallEnabled tracks the debug-tool-call toggle state.
var debugToolCallEnabled bool

// InternalPromptRunner is set by main to allow slash commands to invoke the AI.
var InternalPromptRunner func(prompt string, enableTools bool) (string, error)

// InternalPromptRunnerWithProgress is set by main to allow slash commands to
// invoke the AI with a progress label.
var InternalPromptRunnerWithProgress func(prompt string, enableTools bool, label string) (string, error)

// globalUsageTracker is set by main to enable /cost.
var globalUsageTracker *runtime.UsageTracker

// SetUsageTracker sets the global usage tracker for /cost.
func SetUsageTracker(t *runtime.UsageTracker) {
	globalUsageTracker = t
}

// CommandSpec describes a slash command.
type CommandSpec struct {
	Name        string
	Aliases     []string
	Summary     string
	Args        string // argument hint
	ResumeMode  bool   // works in resume mode
	Handler     func(args string) (string, error)
}

// Commands returns all built-in slash commands.
func Commands() []CommandSpec {
	return []CommandSpec{
		{Name: "help", Summary: "Show help for slash commands", ResumeMode: true, Handler: cmdHelp},
		{Name: "status", Summary: "Show session status", ResumeMode: true, Handler: cmdStatus},
		{Name: "compact", Summary: "Compact conversation history", ResumeMode: true, Handler: cmdCompact},
		{Name: "model", Summary: "Show or change current model", Args: "[model]", Handler: cmdModel},
		{Name: "permissions", Summary: "Show or change permission mode", Args: "[read-only|workspace-write|danger-full-access]", Handler: cmdPermissions},
		{Name: "clear", Summary: "Clear conversation history", Args: "[--confirm]", ResumeMode: true, Handler: cmdClear},
		{Name: "cost", Summary: "Show token usage and estimated cost", ResumeMode: true, Handler: cmdCost},
		{Name: "resume", Summary: "Resume a previous session", Args: "<session-path>", Handler: cmdResume},
		{Name: "config", Summary: "Show or edit configuration", Args: "[env|hooks|model|plugins]", ResumeMode: true, Handler: cmdConfig},
		{Name: "memory", Summary: "Show or manage memory", ResumeMode: true, Handler: cmdMemory},
		{Name: "init", Summary: "Initialize project configuration", Handler: cmdInit},
		{Name: "setup", Summary: "Re-run first-run setup wizard (API key, model, etc.)", ResumeMode: true, Handler: cmdSetup},
		{Name: "diff", Summary: "Show uncommitted changes", Handler: cmdDiff},
		{Name: "version", Summary: "Show version info", ResumeMode: true, Handler: cmdVersion},
		{Name: "commit", Summary: "Generate a commit message and create a git commit", Handler: cmdCommit},
		{Name: "pr", Summary: "Draft or create a pull request", Args: "[context]", Handler: cmdPR},
		{Name: "issue", Summary: "Draft or create a GitHub issue", Args: "[context]", Handler: cmdIssue},
		{Name: "export", Summary: "Export conversation to file", Args: "[file]", ResumeMode: true, Handler: cmdExport},
		{Name: "session", Summary: "Manage sessions", Args: "[list|switch <id>]", Handler: cmdSession},
		{Name: "agents", Summary: "List and manage agents", ResumeMode: true, Handler: cmdAgents},
		{Name: "skills", Summary: "List and manage skills", ResumeMode: true, Handler: cmdSkills},
		{Name: "branch", Summary: "Git branch operations", Args: "[list|create <name>|switch <name>]", Handler: cmdBranch},
		{Name: "worktree", Summary: "Git worktree operations", Args: "[list|add <path>|remove <path>]", Handler: cmdWorktree},
		{Name: "todo", Summary: "Show current todo list", ResumeMode: true, Handler: cmdTodo},
		{Name: "plugin", Aliases: []string{"plugins", "marketplace"}, Summary: "Manage plugins", Args: "[list|install|enable|disable|uninstall]", Handler: cmdPlugin},
		{Name: "debug-tool-call", Summary: "Toggle tool call debugging", ResumeMode: true, Handler: cmdDebugToolCall},
		{Name: "bughunter", Summary: "Inspect the codebase for likely bugs", Args: "[scope]", Handler: cmdBughunter},
		{Name: "commit-push-pr", Summary: "Commit, push, and create a PR", Args: "[context]", Handler: cmdCommitPushPR},
		{Name: "ultraplan", Summary: "Deep planning with multi-step reasoning", Args: "[task]", Handler: cmdUltraplan},
		{Name: "teleport", Summary: "Jump to a file or symbol by searching the workspace", Args: "<query>", Handler: cmdTeleport},
		{Name: "fast", Summary: "Toggle fast model (cycle opus -> sonnet -> haiku)", ResumeMode: true, Handler: cmdFast},
		{Name: "undo", Summary: "Undo the last change or revert a commit", ResumeMode: true, Handler: cmdUndo},
		{Name: "doctor", Summary: "Run diagnostics and check environment", ResumeMode: true, Handler: cmdDoctor},
		{Name: "context", Summary: "Show context window usage", ResumeMode: true, Handler: cmdContext},
		{Name: "review-pr", Summary: "Review a pull request", Args: "[PR number or URL]", Handler: cmdReviewPR},
		{Name: "vim", Summary: "Toggle vim keybinding mode in input", ResumeMode: true, Handler: cmdVim},
		{Name: "statusline", Summary: "Configure status line display", ResumeMode: true, Handler: cmdStatusLine},
		{Name: "grep-tool", Summary: "Search tool definitions by name or keyword", Args: "<query>", ResumeMode: true, Handler: cmdGrepTool},
		{Name: "mcp", Summary: "Manage MCP server connections", Args: "[list|restart <name>]", ResumeMode: true, Handler: cmdMCP},
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
	fmt.Fprintf(&buf, "Go-Claw-Code v%s\n", Version)
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&buf, "  Working dir: %s\n", cwd)
	}
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  Branch: %s\n", strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(&buf, "  Agents: %d running\n", len(tools.GetAgents()))
	fmt.Fprintf(&buf, "  Todos: %d\n", len(tools.GetTodos()))
	if globalUsageTracker != nil {
		fmt.Fprintf(&buf, "  Usage: %s\n", globalUsageTracker.Summary())
	}
	if debugToolCallEnabled {
		buf.WriteString("  Debug tool calls: ON\n")
	}
	// Show remote context
	ctx := runtime.DetectRemoteContext()
	if ctx.IsRemote() {
		fmt.Fprintf(&buf, "  Remote: %s\n", ctx.SessionType())
	}
	return buf.String(), nil
}

func cmdCompact(args string) (string, error) {
	if globalSessionAccessor != nil {
		globalSessionAccessor.Clear()
	}
	return "Conversation history compacted.", nil
}

func cmdModel(args string) (string, error) {
	if args != "" {
		if globalRuntimeControl != nil {
			globalRuntimeControl.SetModel(args)
			return fmt.Sprintf("Model set to: %s", args), nil
		}
		return fmt.Sprintf("Model set to: %s", args), nil
	}
	current := os.Getenv("CLAW_MODEL")
	if current == "" {
		current = os.Getenv("ANTHROPIC_MODEL")
	}
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
		if globalRuntimeControl != nil {
			globalRuntimeControl.SetPermissionMode(args)
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
	if globalSessionAccessor != nil {
		globalSessionAccessor.Clear()
		return "Session cleared.", nil
	}
	return "Session cleared.", nil
}

func cmdCost(args string) (string, error) {
	if globalUsageTracker == nil {
		return "Cost tracking: no usage data available yet.", nil
	}
	return globalUsageTracker.Summary(), nil
}

func cmdResume(args string) (string, error) {
	if args == "" {
		// List available sessions
		entries, err := os.ReadDir(".claw-sessions")
		if err != nil || len(entries) == 0 {
			return "", fmt.Errorf("usage: /resume <session-path>")
		}
		var names []string
		for _, e := range entries {
			info, _ := e.Info()
			names = append(names, fmt.Sprintf("%s (%s)", e.Name(), info.ModTime().Format("2006-01-02 15:04")))
		}
		return "Available sessions:\n  " + strings.Join(names, "\n  ") + "\n\nUsage: /resume <path>", nil
	}
	if _, err := os.Stat(args); err != nil {
		return "", fmt.Errorf("session file not found: %s", args)
	}
	// Return a special marker that main.go can detect for actual resume
	return "RESUME:" + args, nil
}

func cmdConfig(args string) (string, error) {
	switch args {
	case "env":
		return fmt.Sprintf("CLAW_BASE_URL=%s\nCLAW_API_KEY=%s\nANTHROPIC_BASE_URL=%s\nANTHROPIC_API_KEY=%s\nCLAW_PERMISSION_MODE=%s",
			os.Getenv("CLAW_BASE_URL"),
			maskKey(os.Getenv("CLAW_API_KEY")),
			os.Getenv("ANTHROPIC_BASE_URL"),
			maskKey(os.Getenv("ANTHROPIC_API_KEY")),
			os.Getenv("CLAW_PERMISSION_MODE")), nil
	case "hooks":
		return "Hooks are configured in .claw/settings.json under \"hooks\".\nUse /config to view current settings.", nil
	case "model":
		model := os.Getenv("CLAW_MODEL")
		if model == "" {
			model = os.Getenv("ANTHROPIC_MODEL")
		}
		if model == "" {
			model = "(default: claude-sonnet-4-6)"
		}
		return fmt.Sprintf("Model: %s", model), nil
	case "plugins":
		home, _ := os.UserHomeDir()
		cfg := plugins.NewPluginManagerConfig(home)
		mgr := plugins.NewManager(cfg)
		list, err := mgr.ListPlugins()
		if err != nil {
			return "", fmt.Errorf("failed to list plugins: %w", err)
		}
		if len(list) == 0 {
			return "No plugins installed.", nil
		}
		var buf strings.Builder
		for _, p := range list {
			status := "disabled"
			if p.Enabled {
				status = "enabled"
			}
			fmt.Fprintf(&buf, "  %s v%s [%s] - %s\n", p.Metadata.Name, p.Metadata.Version, status, p.Metadata.Description)
		}
		return buf.String(), nil
	default:
		return "Usage: /config [env|hooks|model|plugins]", nil
	}
}

func cmdMemory(args string) (string, error) {
	home, _ := os.UserHomeDir()
	var sections []string

	for _, dir := range []string{".claw"} {
		memDir := filepath.Join(dir, "memory")
		if entries, err := os.ReadDir(memDir); err == nil && len(entries) > 0 {
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
			}
			sections = append(sections, fmt.Sprintf("Project memory (%s):\n  %s", dir, strings.Join(files, "\n  ")))
		}
	}

	for _, dir := range []string{".go-claw"} {
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
	if err := os.MkdirAll(".claw", 0755); err != nil {
		return "", err
	}

	created := []string{}

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

	clawMD := "CLAW.md"
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

func cmdSetup(args string) (string, error) {
	if !clawauth.IsTerminal() {
		return "", fmt.Errorf("/setup requires an interactive terminal")
	}

	// Show current config first
	var buf strings.Builder
	buf.WriteString("Current configuration:\n")
	if key := os.Getenv("CLAW_API_KEY"); key != "" {
		buf.WriteString("  CLAW_API_KEY: " + maskKey(key) + "\n")
	} else if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		buf.WriteString("  ANTHROPIC_API_KEY: " + maskKey(key) + " (fallback)\n")
	} else if clawauth.HasCredentialsFile() {
		buf.WriteString("  API Key: saved in ~/.go-claw/auth.json\n")
	} else {
		buf.WriteString("  API Key: not configured\n")
	}
	if u := os.Getenv("CLAW_BASE_URL"); u != "" {
		buf.WriteString("  CLAW_BASE_URL: " + u + "\n")
	} else if u := os.Getenv("ANTHROPIC_BASE_URL"); u != "" {
		buf.WriteString("  ANTHROPIC_BASE_URL: " + u + " (fallback)\n")
	}
	if m := os.Getenv("CLAW_MODEL"); m != "" {
		buf.WriteString("  CLAW_MODEL: " + m + "\n")
	} else if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		buf.WriteString("  ANTHROPIC_MODEL: " + m + " (fallback)\n")
	}
	buf.WriteString("\n")

	// Run the setup wizard
	result, err := clawauth.RunSetupWizard(Version)
	if err != nil {
		return buf.String() + "Setup failed: " + err.Error(), nil
	}
	if result == nil {
		return buf.String() + "Setup skipped.", nil
	}

	// Apply changes to runtime if possible
	if globalRuntimeControl != nil {
		if result.Model != "" {
			globalRuntimeControl.SetModel(result.Model)
		}
	}

	buf.WriteString("Setup complete! ")
	if result.APIKey != "" {
		buf.WriteString("Credentials saved to ~/.go-claw/auth.json. ")
	}
	if result.OAuthToken {
		buf.WriteString("OAuth token saved. ")
	}
	buf.WriteString("\nNote: restart Go-Claw-Code for all changes to take full effect.")
	return buf.String(), nil
}

func cmdDiff(args string) (string, error) {
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
	return fmt.Sprintf("Go-Claw-Code v%s", Version), nil
}

func cmdCommit(args string) (string, error) {
	changes, _ := exec.Command("git", "diff", "--stat", "HEAD").CombinedOutput()
	statusOut, _ := exec.Command("git", "status", "--short").CombinedOutput()

	commitMsg := "claw: automated commit"
	if args != "" {
		commitMsg = args
	} else if len(statusOut) > 0 {
		lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
		if len(lines) > 0 {
			commitMsg = fmt.Sprintf("claw: update %d file(s)", len(lines))
		}
	}

	var buf strings.Builder
	if len(changes) > 0 {
		buf.WriteString("Changes to commit:\n")
		buf.WriteString(string(changes))
		buf.WriteString("\n")
	}

	exec.Command("git", "add", "-A").Run()

	cmd := exec.Command("git", "commit", "-m", commitMsg)
	out, err := cmd.CombinedOutput()
	buf.Write(out)
	if err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func cmdPR(args string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found. Install: https://cli.github.com")
	}

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

	sessionDir := ".claw-sessions"
	data, err := findLatestSession(sessionDir)
	if err != nil {
		content := fmt.Sprintf("# Go-Claw-Code Export\n\nExported: %s\n\nNo session data found.\n", time.Now().Format(time.RFC3339))
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
		if err != nil || len(entries) == 0 {
			return "No saved sessions. Use /export or run a conversation first.", nil
		}
		var names []string
		for _, e := range entries {
			info, _ := e.Info()
			names = append(names, fmt.Sprintf("%s (%s)", e.Name(), info.ModTime().Format("2006-01-02 15:04")))
		}
		return "Sessions:\n  " + strings.Join(names, "\n  "), nil
	}
	if parts[0] == "switch" && len(parts) >= 2 {
		sessionPath := filepath.Join(".claw-sessions", parts[1])
		if _, err := os.Stat(sessionPath); err != nil {
			sessionPath = parts[1]
		}
		return "RESUME:" + sessionPath, nil
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
		return "No skills found. Create skills in .claw/skills/ or ~/.go-claw/skills/", nil
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
	home, _ := os.UserHomeDir()
	cfg := plugins.NewPluginManagerConfig(filepath.Join(home, ".go-claw"))
	mgr := plugins.NewManager(cfg)
	parts := strings.Fields(args)

	switch {
	case len(parts) == 0 || parts[0] == "list":
		list, err := mgr.ListPlugins()
		if err != nil {
			return "", fmt.Errorf("failed to list plugins: %w", err)
		}
		if len(list) == 0 {
			return "No plugins installed. Place plugins in .claw/plugins/ or ~/.go-claw/plugins/", nil
		}
		var buf strings.Builder
		for _, p := range list {
			status := "disabled"
			if p.Enabled {
				status = "enabled"
			}
			fmt.Fprintf(&buf, "  %s v%s [%s] - %s\n", p.Metadata.Name, p.Metadata.Version, status, p.Metadata.Description)
		}
		return buf.String(), nil

	case parts[0] == "install" && len(parts) >= 2:
		if _, err := mgr.Install(parts[1]); err != nil {
			return "", fmt.Errorf("install failed: %w", err)
		}
		return fmt.Sprintf("Plugin installed from: %s", parts[1]), nil

	case parts[0] == "enable" && len(parts) >= 2:
		if err := mgr.Enable(parts[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Plugin enabled: %s", parts[1]), nil

	case parts[0] == "disable" && len(parts) >= 2:
		if err := mgr.Disable(parts[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Plugin disabled: %s", parts[1]), nil

	case parts[0] == "uninstall" && len(parts) >= 2:
		if err := mgr.Uninstall(parts[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Plugin uninstalled: %s", parts[1]), nil

	default:
		return "Usage: /plugin [list|install <source>|enable <name>|disable <name>|uninstall <name>]", nil
	}
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
	if InternalPromptRunner != nil {
		return InternalPromptRunner(fmt.Sprintf("Inspect the codebase at %s for likely bugs. Read files, search for common bug patterns (off-by-one errors, null pointer dereferences, race conditions, unhandled errors, resource leaks), and report your findings.", scope), true)
	}
	return fmt.Sprintf("Bughunter: inspecting %s for likely bugs.\nUse tools to read files, search for patterns, and identify potential issues.", scope), nil
}

func cmdCommitPushPR(args string) (string, error) {
	var buf strings.Builder

	commitMsg := "claw: automated commit"
	if args != "" {
		commitMsg = args
	}

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

	branch, _ := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	branchName := strings.TrimSpace(string(branch))

	out, err = exec.Command("git", "push", "-u", "origin", branchName).CombinedOutput()
	buf.WriteString("\n2. Push:\n")
	buf.WriteString(string(out))
	if err != nil {
		buf.WriteString("(push may have failed -- branch may already be up to date)\n")
	}

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
	if InternalPromptRunner != nil {
		return InternalPromptRunner(fmt.Sprintf("Create a detailed implementation plan for the following task. Break it down into steps, identify files to modify, consider edge cases, and provide a clear execution order:\n\n%s", task), true)
	}
	return fmt.Sprintf("Ultraplan: deep planning for: %s", task), nil
}

func cmdTeleport(args string) (string, error) {
	if args == "" {
		return "", fmt.Errorf("usage: /teleport <file-or-symbol>")
	}

	var results []string

	if _, err := os.Stat(args); err == nil {
		results = append(results, args+" (file)")
	}

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

func cmdFast(args string) (string, error) {
	if globalRuntimeControl == nil {
		return "", fmt.Errorf("runtime not initialized")
	}
	current := strings.ToLower(globalRuntimeControl.Model())
	switch {
	case strings.Contains(current, "opus"):
		globalRuntimeControl.SetModel("claude-sonnet-4-6")
	case strings.Contains(current, "sonnet"):
		globalRuntimeControl.SetModel("claude-haiku-4-5")
	default:
		globalRuntimeControl.SetModel("claude-sonnet-4-6")
	}
	return fmt.Sprintf("Switched to: %s", globalRuntimeControl.Model()), nil
}

func cmdUndo(args string) (string, error) {
	// Try git undo first
	if _, err := exec.Command("git", "rev-parse", "--is-inside-work-tree").CombinedOutput(); err == nil {
		if args == "--hard" {
			out, err := exec.Command("git", "reset", "--hard", "HEAD~1").CombinedOutput()
			return string(out), err
		}
		out, err := exec.Command("git", "reset", "--soft", "HEAD~1").CombinedOutput()
		if err != nil {
			return string(out), err
		}
		return "Undid last commit (soft reset). Changes are staged.\nUse /undo --hard to discard changes entirely.", nil
	}
	return "", fmt.Errorf("not in a git repository")
}

func cmdDoctor(args string) (string, error) {
	var buf strings.Builder
	fmt.Fprintf(&buf, "Go-Claw-Code v%s Diagnostics:\n", Version)

	// Check Go version
	if out, err := exec.Command("go", "version").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  [OK] Go: %s", strings.TrimSpace(string(out)))
	} else {
		buf.WriteString("  [FAIL] Go not found\n")
	}

	// Check git
	if out, err := exec.Command("git", "--version").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  [OK] Git: %s\n", strings.TrimSpace(string(out)))
	} else {
		buf.WriteString("  [FAIL] Git not found\n")
	}

	// Check gh CLI
	if _, err := exec.LookPath("gh"); err == nil {
		buf.WriteString("  [OK] GitHub CLI: installed\n")
	} else {
		buf.WriteString("  [WARN] GitHub CLI not found (optional)\n")
	}

	// Check API key
	apiKey := os.Getenv("CLAW_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey != "" {
		fmt.Fprintf(&buf, "  [OK] API Key: %s...%s\n", apiKey[:4], apiKey[len(apiKey)-4:])
	} else {
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, ".go-claw", "auth.json")); err == nil {
			buf.WriteString("  [OK] API Key: (saved in ~/.go-claw/auth.json)\n")
		} else {
			buf.WriteString("  [WARN] No API key set (CLAW_API_KEY or ANTHROPIC_API_KEY)\n")
		}
	}

	// Check base URL
	baseURL := os.Getenv("CLAW_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("ANTHROPIC_BASE_URL")
	}
	if baseURL != "" {
		fmt.Fprintf(&buf, "  [INFO] Base URL: %s\n", baseURL)
	}

	// Check config file
	if _, err := os.Stat(".claw/settings.json"); err == nil {
		buf.WriteString("  [OK] Project config: .claw/settings.json\n")
	} else {
		buf.WriteString("  [INFO] No project config (use /init to create)\n")
	}

	// Check CLAW.md
	if _, err := os.Stat("CLAW.md"); err == nil {
		buf.WriteString("  [OK] Project instructions: CLAW.md\n")
	}

	// Check permissions
	permMode := os.Getenv("CLAW_PERMISSION_MODE")
	if permMode == "" {
		permMode = "danger-full-access"
	}
	fmt.Fprintf(&buf, "  [INFO] Permission mode: %s\n", permMode)

	return buf.String(), nil
}

func cmdContext(args string) (string, error) {
	var buf strings.Builder
	buf.WriteString("Context:\n")

	// Working directory
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&buf, "  Working dir: %s\n", cwd)
	}

	// Git info
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  Branch: %s\n", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").CombinedOutput(); err == nil {
		fmt.Fprintf(&buf, "  Commit: %s\n", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "status", "--porcelain").CombinedOutput(); err == nil {
		count := len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		if string(out) == "" {
			count = 0
		}
		fmt.Fprintf(&buf, "  Uncommitted files: %d\n", count)
	}

	// Model
	model := os.Getenv("CLAW_MODEL")
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = "(default)"
	}
	fmt.Fprintf(&buf, "  Model: %s\n", model)

	// Agents
	fmt.Fprintf(&buf, "  Agents: %d running\n", len(tools.GetAgents()))

	// Todos
	fmt.Fprintf(&buf, "  Todos: %d\n", len(tools.GetTodos()))

	// Context window usage
	if globalUsageTracker != nil {
		fmt.Fprintf(&buf, "  Usage: %s\n", globalUsageTracker.Summary())
	}

	// Context files
	contextFiles := []string{"CLAW.md", ".claw/settings.json", ".claw/memory/MEMORY.md"}
	for _, f := range contextFiles {
		if _, err := os.Stat(f); err == nil {
			fmt.Fprintf(&buf, "  Context file: %s\n", f)
		}
	}

	return buf.String(), nil
}

func cmdReviewPR(args string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found. Install: https://cli.github.com")
	}
	prRef := args
	if prRef == "" {
		// Try current branch's PR
		out, err := exec.Command("gh", "pr", "view", "--json", "number,title,url").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("no PR found for current branch. Use /review-pr <number|URL>")
		}
		prRef = strings.TrimSpace(string(out))
	}

	// Get PR diff
	out, err := exec.Command("gh", "pr", "diff", prRef).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get PR diff: %w", err)
	}
	if InternalPromptRunner != nil {
		return InternalPromptRunner(fmt.Sprintf("Review the following pull request diff and provide a structured code review. Identify bugs, suggest improvements, check for security issues, and assess overall code quality:\n\n%s", string(out)), true)
	}
	return fmt.Sprintf("PR diff for %s:\n%s", prRef, string(out)), nil
}

func cmdVim(args string) (string, error) {
	if globalRuntimeControl == nil {
		return "", fmt.Errorf("runtime not initialized")
	}
	current := globalRuntimeControl.Setting("vim_mode")
	if current == "true" {
		globalRuntimeControl.SetSetting("vim_mode", "false")
		return "Vim mode disabled.", nil
	}
	globalRuntimeControl.SetSetting("vim_mode", "true")
	return "Vim mode enabled. Use hjkl for navigation, i for insert, Esc for normal mode.", nil
}

func cmdStatusLine(args string) (string, error) {
	if globalRuntimeControl == nil {
		return "", fmt.Errorf("runtime not initialized")
	}
	switch args {
	case "show":
		globalRuntimeControl.SetSetting("statusline_visible", "true")
		return "Status line visible.", nil
	case "hide":
		globalRuntimeControl.SetSetting("statusline_visible", "false")
		return "Status line hidden.", nil
	case "":
		visible := globalRuntimeControl.Setting("statusline_visible")
		if visible == "" {
			visible = "true"
		}
		return fmt.Sprintf("Status line: %s\nUse /statusline show|hide to toggle.", visible), nil
	default:
		return "Usage: /statusline [show|hide]", nil
	}
}

func cmdGrepTool(args string) (string, error) {
	if args == "" {
		return "Usage: /grep-tool <query>\nSearches tool names and descriptions.", nil
	}
	// Delegate to ToolSearch tool
	if InternalPromptRunner != nil {
		result, err := InternalPromptRunner(fmt.Sprintf("Use the ToolSearch tool to search for: %s. Show me the matching tools with their names and descriptions.", args), false)
		if err != nil {
			return "", err
		}
		return result, nil
	}

	// Fallback: manual search through registered tools
	var buf strings.Builder
	for _, cmd := range Commands() {
		_ = cmd
	}
	fmt.Fprintf(&buf, "Search for '%s' in tools.\nUse ToolSearch tool for full search.", args)
	return buf.String(), nil
}

func cmdMCP(args string) (string, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 || parts[0] == "list" {
		// Check for MCP config
		cfgData, err := os.ReadFile(".claw/settings.json")
		if err != nil {
			return "No MCP servers configured. Add servers to .claw/settings.json under \"mcpServers\".", nil
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal(cfgData, &cfg); err != nil {
			return "", fmt.Errorf("failed to parse settings: %w", err)
		}
		servers, ok := cfg["mcpServers"].(map[string]interface{})
		if !ok || len(servers) == 0 {
			return "No MCP servers configured. Add servers to .claw/settings.json under \"mcpServers\".", nil
		}
		var buf strings.Builder
		buf.WriteString("MCP Servers:\n")
		for name, srv := range servers {
			status := "configured"
			if smap, ok := srv.(map[string]interface{}); ok {
				if cmd, ok := smap["command"].(string); ok {
					fmt.Fprintf(&buf, "  %s: %s (%s)\n", name, cmd, status)
					continue
				}
				if url, ok := smap["url"].(string); ok {
					fmt.Fprintf(&buf, "  %s: %s (%s)\n", name, url, status)
					continue
				}
			}
			fmt.Fprintf(&buf, "  %s (%s)\n", name, status)
		}
		return buf.String(), nil
	}

	if parts[0] == "restart" && len(parts) >= 2 {
		return fmt.Sprintf("MCP server restart requested for: %s\nRestart MCP servers by restarting claw.", parts[1]), nil
	}

	return "Usage: /mcp [list|restart <name>]", nil
}

// --- RuntimeControl Adapter ---

// RuntimeControlAdapter wraps a ConversationRuntime to expose runtime control
// methods to slash commands without importing the runtime package directly.
type RuntimeControlAdapter struct {
	rt *runtime.ConversationRuntime
}

// NewRuntimeControlAdapter creates a new adapter around the given runtime.
func NewRuntimeControlAdapter(rt *runtime.ConversationRuntime) *RuntimeControlAdapter {
	return &RuntimeControlAdapter{rt: rt}
}

// PermissionMode returns the current permission mode from the underlying runtime.
func (a *RuntimeControlAdapter) PermissionMode() string {
	if a.rt == nil {
		return ""
	}
	return a.rt.PermissionMode()
}

// SetPermissionMode sets the permission mode on the underlying runtime.
func (a *RuntimeControlAdapter) SetPermissionMode(mode string) {
	if a.rt == nil {
		return
	}
	a.rt.SetPermissionMode(mode)
}

// SetSetting stores a key-value setting in the underlying runtime.
func (a *RuntimeControlAdapter) SetSetting(key, value string) {
	if a.rt == nil {
		return
	}
	a.rt.SetSetting(key, value)
}

// Setting retrieves a setting value from the underlying runtime.
func (a *RuntimeControlAdapter) Setting(key string) string {
	if a.rt == nil {
		return ""
	}
	return a.rt.Setting(key)
}

// Model returns the current model name.
func (a *RuntimeControlAdapter) Model() string {
	if a.rt == nil {
		return ""
	}
	return a.rt.Model()
}

// SetModel changes the model on the underlying runtime.
func (a *RuntimeControlAdapter) SetModel(m string) {
	if a.rt == nil {
		return
	}
	a.rt.SetModel(m)
}

// globalRuntimeControl holds the wired RuntimeControlAdapter for slash commands.
var globalRuntimeControl *RuntimeControlAdapter

// SetRuntimeControl sets the global runtime control adapter.
func SetRuntimeControl(adapter *RuntimeControlAdapter) {
	globalRuntimeControl = adapter
}

// GetRuntimeControl returns the global runtime control adapter.
func GetRuntimeControl() *RuntimeControlAdapter {
	return globalRuntimeControl
}

// SessionAccessor provides access to session operations for slash commands.
type SessionAccessor interface {
	MessageCount() int
	SaveSession(path string) error
	Clear()
}

// globalSessionAccessor holds the wired SessionAccessor.
var globalSessionAccessor SessionAccessor

// SetSessionAccessor sets the global session accessor.
func SetSessionAccessor(sa SessionAccessor) {
	globalSessionAccessor = sa
}

// GetSessionAccessor returns the global session accessor.
func GetSessionAccessor() SessionAccessor {
	return globalSessionAccessor
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

// marshalIndent is a helper for JSON formatting.
func marshalIndent(v interface{}) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data)
}
