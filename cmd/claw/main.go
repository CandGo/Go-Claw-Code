package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ergochat/readline"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/go-claw/claw/internal/api"
	"github.com/go-claw/claw/internal/auth"
	"github.com/go-claw/claw/internal/commands"
	"github.com/go-claw/claw/internal/config"
	"github.com/go-claw/claw/internal/mcp"
	"github.com/go-claw/claw/internal/plugins"
	"github.com/go-claw/claw/internal/runtime"
	"github.com/go-claw/claw/internal/tools"
	"github.com/go-claw/claw/internal/tui"
)

const version = "0.3.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\nRun `claw --help` for usage.\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		modelFlag    string
		permFlag     string
		outputFormat string
		uiMode       string
		showVersion  bool
		showPrompt   bool
		showAgents   bool
		showSkills   bool
		showConfig   bool
		doAuth       bool
		mcpFlag      string
	)

	fs := flag.NewFlagSet("claw", flag.ExitOnError)
	fs.StringVar(&modelFlag, "model", "", "Override the active model")
	fs.StringVar(&permFlag, "permission-mode", "", "Permission mode: read-only, workspace-write, danger-full-access")
	fs.StringVar(&outputFormat, "output-format", "text", "Non-interactive output format: text or json")
	fs.StringVar(&uiMode, "ui", "auto", "UI mode: tui, repl, auto")
	fs.BoolVar(&showVersion, "version", false, "Print version")
	fs.BoolVar(&showPrompt, "system-prompt", false, "Print system prompt")
	fs.BoolVar(&showAgents, "agents", false, "List agents")
	fs.BoolVar(&showSkills, "skills", false, "List skills")
	fs.BoolVar(&showConfig, "show-config", false, "Show configuration sources")
	fs.BoolVar(&doAuth, "auth", false, "Run OAuth authentication flow")
	fs.StringVar(&mcpFlag, "mcp", "", "Start MCP server (stdio)")
	fs.Parse(os.Args[1:])

	if showVersion {
		fmt.Printf("Claw Code (Go)\n  Version: %s\n  Phase: 3 (TUI + OAuth + MCP + Plugins)\n", version)
		return nil
	}

	if showPrompt {
		fmt.Print(runtime.DefaultSystemPrompt())
		return nil
	}

	if doAuth {
		return runAuth()
	}

	if showAgents {
		agents := tools.GetAgents()
		if len(agents) == 0 {
			fmt.Println("No agents running.")
		} else {
			for _, a := range agents {
				fmt.Printf("  %s [%s] %s - %s\n", a.ID, a.Status, a.Type, a.Description)
			}
		}
		return nil
	}

	if showSkills {
		skills := tools.DiscoverSkills()
		if len(skills) == 0 {
			fmt.Println("No skills found. Create skills in .claw/skills/ or ~/.claw/skills/")
		} else {
			for _, s := range skills {
				fmt.Printf("  - %s\n", s)
			}
		}
		return nil
	}

	if showConfig {
		fmt.Println("Configuration sources:")
		fmt.Print(config.DescribeSources())
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Printf("\nEffective config:\n")
		fmt.Printf("  Model: %s\n", cfg.Model)
		fmt.Printf("  Permission mode: %s\n", cfg.PermissionMode)
		fmt.Printf("  Pre-tool hooks: %d\n", len(cfg.Hooks.PreToolUse))
		fmt.Printf("  Post-tool hooks: %d\n", len(cfg.Hooks.PostToolUse))
		fmt.Printf("  MCP servers: %d\n", len(cfg.MCPServers))
		return nil
	}

	// MCP stdio mode
	if mcpFlag != "" {
		return runMCPStdio(mcpFlag)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve model
	model := resolveArg(modelFlag, "ANTHROPIC_MODEL", cfg.Model)
	model = api.ResolveModelAlias(model)

	// Resolve auth
	apiKey, bearerToken, err := auth.ResolveAuthWithOAuth()
	if err != nil {
		return err
	}
	apiAuth := &api.AuthSource{APIKey: apiKey, BearerToken: bearerToken}

	baseURL := api.BaseURL()

	// Permission mode
	permMode := resolveArg(permFlag, "CLAW_PERMISSION_MODE", cfg.PermissionMode)
	_ = permMode

	// Create provider
	provider := api.NewProvider(model, apiAuth, baseURL)

	// Create tools
	toolReg := tools.NewToolRegistry()

	// Connect MCP servers
	mcpClients := connectMCPServers(cfg)
	for _, mc := range mcpClients {
		mcpTools, _ := mc.ListTools(context.Background())
		for _, t := range mcpTools {
			// Register MCP tools as dynamic tools
			toolName := t.Name
			toolReg.RegisterDynamic(toolName, t.Description, t.InputSchema, func(input map[string]interface{}) (string, error) {
				return mc.CallTool(context.Background(), toolName, input)
			})
		}
	}

	// Discover plugins
	pluginMgr := plugins.NewManager()
	pluginMgr.Discover()
	pluginTools := pluginMgr.AllTools()
	for _, t := range pluginTools {
		toolReg.RegisterDynamic(t.Name, t.Description, t.InputSchema, func(input map[string]interface{}) (string, error) {
			return executePluginTool(pluginMgr, t.Name, input)
		})
	}

	// Create hook runner (config + plugin hooks)
	pluginHooks := pluginMgr.AllHooks()
	allPre := append([]string{}, cfg.Hooks.PreToolUse...)
	allPre = append(allPre, pluginHooks.PreToolUse...)
	allPost := append([]string{}, cfg.Hooks.PostToolUse...)
	allPost = append(allPost, pluginHooks.PostToolUse...)
	_ = runtime.NewHookRunner(allPre, allPost)

	// Create runtime
	rt := runtime.NewConversationRuntime(provider, toolReg, model)

	// Determine prompt from args
	args := fs.Args()
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		return runOnce(rt, prompt, outputFormat)
	}

	// Choose UI mode
	useTUI := uiMode == "tui" || (uiMode == "auto" && isTerminal())

	if useTUI {
		return runTUI(rt, cfg)
	}
	return runREPL(rt, cfg)
}

func runOnce(rt *runtime.ConversationRuntime, prompt string, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	outputs, usage, err := rt.RunTurn(ctx, prompt)
	if err != nil {
		return err
	}

	for _, out := range outputs {
		switch out.Type {
		case "text":
			fmt.Println(out.Text)
		case "tool_use":
			fmt.Fprintf(os.Stderr, "  tool: %s\n", out.ToolName)
		case "tool_result":
			fmt.Println(out.Text)
		}
	}

	if usage != nil {
		fmt.Fprintf(os.Stderr, "\n  tokens: in=%d out=%d\n", usage.InputTokens, usage.OutputTokens)
	}
	return nil
}

func runTUI(rt *runtime.ConversationRuntime, cfg *config.Config) error {
	model := tui.NewModel(rt)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func runREPL(rt *runtime.ConversationRuntime, cfg *config.Config) error {
	fmt.Println("  Claw Code (Go) v" + version)
	fmt.Printf("  Model: %s\n", rt.Model())
	fmt.Printf("  Permission: %s\n", cfg.PermissionMode)
	fmt.Println("  Type /help for commands, Ctrl+D to exit")
	fmt.Println()

	rl, err := readline.New("> ")
	if err != nil {
		return fmt.Errorf("failed to init readline: %w", err)
	}
	defer rl.Close()

	compactionCfg := runtime.DefaultCompactionConfig()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF || err == readline.ErrInterrupt {
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle slash commands
		if strings.HasPrefix(line, "/") {
			cmd, cmdArgs := commands.Parse(line)
			if cmd == nil {
				fmt.Printf("Unknown command: /%s\n", cmdArgs)
				continue
			}
			if cmd.Name == "quit" || cmd.Name == "exit" {
				break
			}
			output, err := cmd.Handler(cmdArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			} else if output != "" {
				fmt.Println(output)
			}
			if cmd.Name == "compact" {
				if rt.MessageCount() > compactionCfg.PreserveRecent {
					rt.Compact(compactionCfg)
					fmt.Printf("Compacted. Messages: %d\n", rt.MessageCount())
				} else {
					fmt.Println("Not enough messages to compact.")
				}
			}
			continue
		}

		// Check compaction
		if rt.ShouldCompact(compactionCfg) {
			rt.Compact(compactionCfg)
			fmt.Fprintf(os.Stderr, "  [auto-compacted, messages: %d]\n", rt.MessageCount())
		}

		// Run turn
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		outputs, usage, err := rt.RunTurn(ctx, line)
		cancel()

		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}

		for _, out := range outputs {
			switch out.Type {
			case "text":
				fmt.Println(out.Text)
			case "tool_use":
				fmt.Printf("  \033[36mtool: %s\033[0m\n", out.ToolName)
			case "tool_result":
				if out.IsError {
					fmt.Printf("  \033[31merror: %s\033[0m\n", out.Text)
				}
			}
		}

		if usage != nil {
			fmt.Fprintf(os.Stderr, "  \033[90mtokens: in=%d out=%d\033[0m\n", usage.InputTokens, usage.OutputTokens)
		}
		fmt.Println()
	}

	return nil
}

func runAuth() error {
	cfg := auth.DefaultOAuthConfig()
	token, err := auth.StartAuthFlow(cfg)
	if err != nil {
		return fmt.Errorf("OAuth failed: %w", err)
	}
	if err := auth.SaveToken(token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}
	fmt.Println("Authentication successful! Token saved.")
	return nil
}

func runMCPStdio(serverName string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	srv, ok := cfg.MCPServers[serverName]
	if !ok {
		return fmt.Errorf("MCP server '%s' not found in config", serverName)
	}

	client, err := mcp.NewClient(context.Background(), srv.Command, []string{}, srv.Env)
	if err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}
	defer client.Close()

	if err := client.Initialize(context.Background()); err != nil {
		return fmt.Errorf("MCP init failed: %w", err)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	fmt.Printf("MCP server '%s' (%s v%s) connected with %d tools:\n", serverName, client.ServerInfo().Name, client.ServerInfo().Version, len(tools))
	for _, t := range tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	return nil
}

func connectMCPServers(cfg *config.Config) []*mcp.Client {
	var clients []*mcp.Client
	for name, srv := range cfg.MCPServers {
		args := []string{}
		client, err := mcp.NewClient(context.Background(), srv.Command, args, srv.Env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [MCP] Failed to connect %s: %v\n", name, err)
			continue
		}
		if err := client.Initialize(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "  [MCP] Init failed for %s: %v\n", name, err)
			client.Close()
			continue
		}
		fmt.Fprintf(os.Stderr, "  [MCP] Connected: %s (%s)\n", name, client.ServerInfo().Name)
		clients = append(clients, client)
	}
	return clients
}

func executePluginTool(mgr *plugins.Manager, name string, input map[string]interface{}) (string, error) {
	// Find the plugin that owns this tool
	for _, p := range mgr.List() {
		if !p.Enabled {
			continue
		}
		for _, t := range p.Tools {
			if t.Name == name && p.Command != "" {
				// Execute plugin command with JSON input
				return "", fmt.Errorf("plugin tool execution not yet implemented for %s", name)
			}
		}
	}
	return "", fmt.Errorf("plugin tool not found: %s", name)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func resolveArg(flag string, env string, def string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}
