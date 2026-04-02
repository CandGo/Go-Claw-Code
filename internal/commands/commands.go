package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-claw/claw/internal/tools"
)

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
			Summary: "Create a git commit",
			Handler: cmdCommit,
		},
		{
			Name:    "pr",
			Summary: "Create a pull request",
			Args:    "[context]",
			Handler: cmdPR,
		},
		{
			Name:    "issue",
			Summary: "Create a GitHub issue",
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
			Aliases:    []string{"plugins"},
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
	// This will be overridden in main.go with actual runtime info
	return "Status: runtime not connected", nil
}

func cmdCompact(args string) (string, error) {
	return "Compaction triggered.", nil
}

func cmdModel(args string) (string, error) {
	if args != "" {
		return fmt.Sprintf("Model set to: %s", args), nil
	}
	return "Use /model <name> to change model", nil
}

func cmdPermissions(args string) (string, error) {
	if args != "" {
		return fmt.Sprintf("Permission mode set to: %s", args), nil
	}
	return "Current mode: danger-full-access", nil
}

func cmdClear(args string) (string, error) {
	return "Session cleared.", nil
}

func cmdCost(args string) (string, error) {
	return "Cost tracking not yet available.", nil
}

func cmdResume(args string) (string, error) {
	if args == "" {
		return "", fmt.Errorf("usage: /resume <session-path>")
	}
	return fmt.Sprintf("Resuming session: %s", args), nil
}

func cmdConfig(args string) (string, error) {
	switch args {
	case "env":
		return fmt.Sprintf("ANTHROPIC_BASE_URL=%s\nANTHROPIC_API_KEY=%s",
			os.Getenv("ANTHROPIC_BASE_URL"),
			maskKey(os.Getenv("ANTHROPIC_API_KEY"))), nil
	case "hooks":
		return "No hooks configured.", nil
	case "model":
		return fmt.Sprintf("Model: %s", os.Getenv("ANTHROPIC_MODEL")), nil
	case "plugins":
		return "No plugins installed.", nil
	default:
		return "Usage: /config [env|hooks|model|plugins]", nil
	}
}

func cmdMemory(args string) (string, error) {
	home, _ := os.UserHomeDir()
	memDir := filepath.Join(home, ".claude", "memory")
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return "No memory files found.", nil
	}
	var files []string
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if len(files) == 0 {
		return "Memory is empty.", nil
	}
	return "Memory files:\n  " + strings.Join(files, "\n  "), nil
}

func cmdInit(args string) (string, error) {
	configFile := ".claw/settings.json"
	if _, err := os.Stat(configFile); err == nil {
		return fmt.Sprintf("Config already exists: %s", configFile), nil
	}
	os.MkdirAll(".claw", 0755)
	content := `{
  "model": "",
  "permissionMode": "danger-full-access",
  "hooks": {
    "PreToolUse": [],
    "PostToolUse": []
  }
}`
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s", configFile), nil
}

func cmdDiff(args string) (string, error) {
	cmd := exec.Command("git", "diff", "--stat")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func cmdVersion(args string) (string, error) {
	return "Claw Code (Go) v0.1.0", nil
}

func cmdCommit(args string) (string, error) {
	// Stage all changes
	exec.Command("git", "add", "-A").Run()
	// Create commit
	cmd := exec.Command("git", "commit", "-m", "claw: automated commit")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cmdPR(args string) (string, error) {
	cmd := exec.Command("gh", "pr", "create", "--fill")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cmdIssue(args string) (string, error) {
	if args == "" {
		args = "New issue"
	}
	cmd := exec.Command("gh", "issue", "create", "--title", args)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cmdExport(args string) (string, error) {
	filename := args
	if filename == "" {
		filename = fmt.Sprintf("claw-export-%d.md", os.Getpid())
	}
	return fmt.Sprintf("Export not yet implemented. Would export to: %s", filename), nil
}

func cmdSession(args string) (string, error) {
	if args == "list" {
		entries, err := os.ReadDir(".claw-sessions")
		if err != nil {
			return "No saved sessions.", nil
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return "Sessions:\n  " + strings.Join(names, "\n  "), nil
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
		fmt.Fprintf(&buf, "  %s [%s] %s - %s\n", a.ID, a.Status, a.Type, a.Description)
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
	default:
		return "", fmt.Errorf("usage: /worktree [list|add <path>|remove <path>]")
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
	return "Plugin system not yet implemented.", nil
}

func cmdDebugToolCall(args string) (string, error) {
	return "Tool call debugging toggled.", nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
