package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ergochat/readline"

	"github.com/go-claw/claw/internal/api"
	clawauth "github.com/go-claw/claw/internal/auth"
	"github.com/go-claw/claw/internal/commands"
	"github.com/go-claw/claw/internal/config"
	"github.com/go-claw/claw/internal/lsp"
	"github.com/go-claw/claw/internal/mcp"
	"github.com/go-claw/claw/internal/plugins"
	"github.com/go-claw/claw/internal/runtime"
	"github.com/go-claw/claw/internal/sandbox"
	"github.com/go-claw/claw/internal/server"
	"github.com/go-claw/claw/internal/tools"
	"github.com/go-claw/claw/internal/tui"
)

const version = "0.5.0"

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
		serveFlag    bool
		servePort    int
		proxyFlag   bool
		sandboxFlag bool
		lspFlag     string
		showVersion bool
		showPrompt  bool
		showConfig  bool
		doAuth      bool
		doLogout    bool
		doInit      bool
		showAgents  bool
		showSkills  bool
		dumpTools   bool
		mcpFlag     string
	)

	fs := flag.NewFlagSet("claw", flag.ExitOnError)
	fs.StringVar(&modelFlag, "model", "", "Override the active model")
	fs.StringVar(&permFlag, "permission-mode", "", "Permission mode: read-only, workspace-write, danger-full-access")
	fs.StringVar(&outputFormat, "output-format", "text", "Non-interactive output format: text or json")
	fs.StringVar(&uiMode, "ui", "auto", "UI mode: tui, repl, auto")
	fs.BoolVar(&serveFlag, "serve", false, "Start HTTP/SSE server mode")
	fs.IntVar(&servePort, "port", 8080, "HTTP server port")
	fs.BoolVar(&proxyFlag, "proxy", false, "Start as API proxy")
	fs.BoolVar(&sandboxFlag, "sandbox", false, "Enable sandbox isolation")
	fs.StringVar(&lspFlag, "lsp", "", "Connect to LSP server (command)")
	fs.BoolVar(&showVersion, "version", false, "Print version")
	fs.BoolVar(&showPrompt, "system-prompt", false, "Print system prompt")
	fs.BoolVar(&doAuth, "login", false, "Run OAuth authentication flow")
	fs.BoolVar(&doLogout, "logout", false, "Clear stored OAuth tokens")
	fs.BoolVar(&doInit, "init", false, "Initialize project configuration")
	fs.BoolVar(&showAgents, "agents", false, "List available agent types")
	fs.BoolVar(&showSkills, "skills", false, "List discovered skills")
	fs.BoolVar(&dumpTools, "dump-tools", false, "Print all tool definitions as JSON")
	fs.StringVar(&mcpFlag, "mcp", "", "Connect to MCP server (name from config)")
	fs.BoolVar(&showConfig, "show-config", false, "Show configuration sources")
	fs.Parse(os.Args[1:])

	// One-shot subcommands
	if showVersion {
		fmt.Printf("Claw Code (Go) v%s\n", version)
		return nil
	}

	if showPrompt {
		fmt.Print(runtime.DefaultSystemPrompt())
		return nil
	}

	if doAuth {
		return runAuth()
	}

	if doLogout {
		return runLogout()
	}

	if doInit {
		_, err := cmdInitProject()
		return err
	}

	if showAgents {
		return listAgentTypes()
	}

	if showSkills {
		return listSkills()
	}

	if dumpTools {
		return dumpToolManifests()
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
		fmt.Printf("  Sandbox: enabled=%v\n", cfg.Sandbox.Enabled)
		fmt.Printf("  Pre-tool hooks: %d\n", len(cfg.Hooks.PreToolUse))
		fmt.Printf("  Post-tool hooks: %d\n", len(cfg.Hooks.PostToolUse))
		fmt.Printf("  MCP servers: %d\n", len(cfg.MCPServers))
		return nil
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
	apiKey, bearerToken, err := clawauth.ResolveAuthWithOAuth()
	if err != nil {
		return err
	}
	apiAuth := &api.AuthSource{APIKey: apiKey, BearerToken: bearerToken}

	baseURL := api.BaseURL()

	// Permission mode
	permMode := resolveArg(permFlag, "CLAW_PERMISSION_MODE", cfg.PermissionMode)
	_ = permMode

	// LSP mode
	if lspFlag != "" {
		return runLSP(lspFlag)
	}

	// MCP mode
	if mcpFlag != "" {
		return runMCP(mcpFlag, cfg)
	}

	// HTTP server mode
	if serveFlag {
		return runServe(cfg, apiAuth, baseURL, model, servePort)
	}

	// Proxy mode
	if proxyFlag {
		return runProxy(apiAuth, baseURL, servePort)
	}

	// Create provider
	provider := api.NewProvider(model, apiAuth, baseURL)

	// Create tools
	toolReg := tools.NewToolRegistry()

	// Setup sandbox (wired into tools)
	if sandboxFlag || cfg.Sandbox.Enabled {
		sbCfg := sandbox.Config{
			Enabled:    true,
			AllowPaths: cfg.Sandbox.AllowPaths,
			AllowNet:   cfg.Sandbox.AllowNet,
		}
		if len(sbCfg.AllowPaths) == 0 {
			sbCfg.AllowPaths = []string{"."}
		}
		if cfg.Sandbox.Isolation > 0 {
			sbCfg.IsolationLevel = cfg.Sandbox.Isolation
		}
		sb := sandbox.New(sbCfg)
		tools.SetSandbox(sb)
		fmt.Fprintf(os.Stderr, "  [sandbox enabled, isolation=%d]\n", sb.IsolationLevel())
	}

	// Connect MCP servers from config
	mcpClients := connectMCPServers(cfg)
	for _, mc := range mcpClients {
		mcpTools, _ := mc.ListTools(context.Background())
		for _, t := range mcpTools {
			toolName := t.Name
			toolReg.RegisterDynamic(toolName, t.Description, t.InputSchema, func(input map[string]interface{}) (string, error) {
				return mc.CallTool(context.Background(), toolName, input)
			})
		}
	}

	// Discover and wire plugins with real execution
	pluginMgr := plugins.NewManager()
	pluginMgr.Discover()
	pluginTools := pluginMgr.AllTools()
	for _, t := range pluginTools {
		toolName := t.Name
		plugin := pluginMgr.GetPluginForTool(toolName)
		toolReg.RegisterDynamic(toolName, t.Description, t.InputSchema, func(input map[string]interface{}) (string, error) {
			if plugin != nil && plugin.Command != "" {
				return plugins.ExecutePluginTool(plugin.Command, input)
			}
			return "", fmt.Errorf("plugin tool %s has no execution command", toolName)
		})
	}

	// Hook runner
	allPre := append([]string{}, cfg.Hooks.PreToolUse...)
	allPost := append([]string{}, cfg.Hooks.PostToolUse...)
	// Add plugin hooks
	pluginHooks := pluginMgr.AllHooks()
	allPre = append(allPre, pluginHooks.PreToolUse...)
	allPost = append(allPost, pluginHooks.PostToolUse...)

	// Create runtime
	rt := runtime.NewConversationRuntime(provider, toolReg, model)
	rt.SetHooks(runtime.NewHookRunner(allPre, allPost))

	// Wire usage tracker into commands
	commands.SetUsageTracker(rt.Usage())

	// Wire agent runtime for sub-agent execution
	tools.SetAgentRuntime(rt)

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
		if rt.Usage() != nil {
			fmt.Fprintf(os.Stderr, "  cost: %s\n", rt.Usage().FormatCost())
		}
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

	// Show remote context
	ctx := runtime.DetectRemoteContext()
	if ctx.IsRemote {
		fmt.Printf("  Remote: %s\n", ctx.SessionType)
	}
	if sandbox.IsContainer() {
		fmt.Println("  Container: detected")
	}

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
				// Handle special command outputs
				if strings.HasPrefix(output, "RESUME:") {
					sessionPath := strings.TrimPrefix(output, "RESUME:")
					if err := resumeSession(rt, sessionPath); err != nil {
						fmt.Fprintf(os.Stderr, "  resume error: %v\n", err)
					} else {
						fmt.Printf("  Resumed session: %s (%d messages)\n", sessionPath, rt.MessageCount())
					}
					continue
				}
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
			if cmd.Name == "clear" {
				rt.Clear()
				fmt.Println("Session cleared.")
			}
			continue
		}

		// Check compaction
		if rt.ShouldCompact(compactionCfg) {
			rt.Compact(compactionCfg)
			fmt.Fprintf(os.Stderr, "  [auto-compacted, messages: %d]\n", rt.MessageCount())
		}

		// Auto-save session
		os.MkdirAll(".claw-sessions", 0755)
		sessionFile := filepath.Join(".claw-sessions", fmt.Sprintf("session-%d.json", time.Now().Unix()))

		// Run turn
		turnCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		outputs, usage, err := rt.RunTurn(turnCtx, line)
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
			cost := ""
			if rt.Usage() != nil {
				cost = fmt.Sprintf(" cost=%s", rt.Usage().FormatCost())
			}
			fmt.Fprintf(os.Stderr, "  \033[90mtokens: in=%d out=%d%s\033[0m\n", usage.InputTokens, usage.OutputTokens, cost)
		}
		fmt.Println()

		// Save session
		rt.SaveSession(sessionFile)
	}

	return nil
}

func resumeSession(rt *runtime.ConversationRuntime, path string) error {
	session, err := runtime.LoadSession(path)
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}
	rt.SetSession(session)
	return nil
}

func runAuth() error {
	cfg := clawauth.DefaultOAuthConfig()
	token, err := clawauth.StartAuthFlow(cfg)
	if err != nil {
		return fmt.Errorf("OAuth failed: %w", err)
	}
	if err := clawauth.SaveToken(token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}
	fmt.Println("Authentication successful! Token saved.")
	return nil
}

func runLogout() error {
	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".claw", "oauth_token.json")
	if err := os.Remove(tokenPath); err != nil {
		return fmt.Errorf("no stored token found")
	}
	fmt.Println("Logged out. Token removed.")
	return nil
}

func cmdInitProject() (string, error) {
	if err := os.MkdirAll(".claw", 0755); err != nil {
		return "", err
	}
	created := []string{}
	settingsFile := ".claw/settings.json"
	if _, err := os.Stat(settingsFile); err != nil {
		content := `{
  "model": "",
  "permissionMode": "danger-full-access",
  "hooks": { "PreToolUse": [], "PostToolUse": [] }
}`
		if err := os.WriteFile(settingsFile, []byte(content), 0644); err != nil {
			return "", err
		}
		created = append(created, settingsFile)
	}
	if _, err := os.Stat("CLAUDE.md"); err != nil {
		if err := os.WriteFile("CLAUDE.md", []byte("# Project Instructions\n"), 0644); err != nil {
			return "", err
		}
		created = append(created, "CLAUDE.md")
	}
	if len(created) == 0 {
		return "Already initialized.", nil
	}
	return "Created: " + strings.Join(created, ", "), nil
}

func listAgentTypes() error {
	types := []struct {
		Name        string
		Tools       string
		MaxIter     int
	}{
		{"Explore", "read-only", 5},
		{"Plan", "read-only + Agent + Todo", 3},
		{"Verification", "bash + read-only (no write/edit)", 10},
		{"claw-guide", "read-only + SendUserMessage", 8},
		{"statusline-setup", "bash + read + write + edit", 10},
		{"general-purpose", "all tools", 32},
	}
	fmt.Println("Agent types:")
	for _, t := range types {
		fmt.Printf("  %-20s tools=%-35s maxIter=%d\n", t.Name, t.Tools, t.MaxIter)
	}
	return nil
}

func listSkills() error {
	skills := tools.DiscoverSkills()
	if len(skills) == 0 {
		fmt.Println("No skills found. Create in .claw/skills/ or ~/.claw/skills/")
		return nil
	}
	fmt.Println("Available skills:")
	for _, s := range skills {
		fmt.Printf("  - %s\n", s)
	}
	return nil
}

func dumpToolManifests() error {
	toolReg := tools.NewToolRegistry()
	defs := toolReg.AvailableTools()
	for _, d := range defs {
		fmt.Printf("--- %s ---\n%s\n\n", d.Name, d.Description)
	}
	return nil
}

func runLSP(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("LSP command required")
	}

	client, err := lsp.NewClient(context.Background(), parts[0], parts[1:])
	if err != nil {
		return fmt.Errorf("LSP connect failed: %w", err)
	}
	defer client.Close()

	result, err := client.Initialize(context.Background(), "file:///"+currentDir())
	if err != nil {
		return fmt.Errorf("LSP initialize failed: %w", err)
	}

	fmt.Printf("LSP server: %s v%s\n", result.ServerInfo.Name, result.ServerInfo.Version)
	fmt.Println("LSP client ready. Use /teleport or /definition commands.")
	return nil
}

func runMCP(name string, cfg *config.Config) error {
	srv, ok := cfg.MCPServers[name]
	if !ok {
		return fmt.Errorf("MCP server '%s' not found in config", name)
	}

	// Support different transport types
	switch srv.Type {
	case "sse", "http":
		return runMCPHTTP(name, srv)
	default: // stdio
		return runMCPStdio(name, srv)
	}
}

func runMCPStdio(name string, srv config.MCPServer) error {
	client, err := mcp.NewClient(context.Background(), srv.Command, srv.Args, srv.Env)
	if err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}
	defer client.Close()

	if err := client.Initialize(context.Background()); err != nil {
		return fmt.Errorf("MCP init failed: %w", err)
	}

	mcpTools, err := client.ListTools(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	fmt.Printf("MCP server '%s' (%s v%s) connected with %d tools:\n", name, client.ServerInfo().Name, client.ServerInfo().Version, len(mcpTools))
	for _, t := range mcpTools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	return nil
}

func runMCPHTTP(name string, srv config.MCPServer) error {
	client, err := mcp.NewHTTPClient(srv.URL, srv.Headers)
	if err != nil {
		return fmt.Errorf("failed to create HTTP MCP client: %w", err)
	}
	defer client.Close()

	if err := client.Initialize(context.Background()); err != nil {
		return fmt.Errorf("MCP HTTP init failed: %w", err)
	}

	mcpTools, err := client.ListTools(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	fmt.Printf("MCP server '%s' (HTTP) connected with %d tools:\n", name, len(mcpTools))
	for _, t := range mcpTools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	return nil
}

func runServe(cfg *config.Config, apiAuth *api.AuthSource, baseURL, model string, port int) error {
	provider := api.NewProvider(model, apiAuth, baseURL)
	toolReg := tools.NewToolRegistry()
	rt := runtime.NewConversationRuntime(provider, toolReg, model)

	srv := server.NewServer(rt, port)
	fmt.Printf("Claw Code server starting on :%d\n", port)
	fmt.Printf("  Model: %s\n", model)
	fmt.Println("  Endpoints: /v1/messages, /v1/messages/stream, /health, /session, /tools")
	return srv.Start()
}

func runProxy(apiAuth *api.AuthSource, baseURL string, port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.ProxyHandler(apiAuth, baseURL))
	fmt.Printf("Claw Code proxy starting on :%d -> %s\n", port, baseURL)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

func connectMCPServers(cfg *config.Config) []*mcp.Client {
	var clients []*mcp.Client
	for name, srv := range cfg.MCPServers {
		switch srv.Type {
		case "sse", "http":
			// HTTP/SSE clients handled separately in MCP mode
			fmt.Fprintf(os.Stderr, "  [MCP] Skipping HTTP server '%s' (use --mcp flag)\n", name)
		default: // stdio
			client, err := mcp.NewClient(context.Background(), srv.Command, srv.Args, srv.Env)
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
	}
	return clients
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func resolveArg(flagVal, env, def string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

func currentDir() string {
	dir, _ := os.Getwd()
	return dir
}
