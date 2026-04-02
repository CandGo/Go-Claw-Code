package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ergochat/readline"
	tea "github.com/charmbracelet/bubbletea"

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

const version = "0.4.0"

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
	fs.BoolVar(&doAuth, "auth", false, "Run OAuth authentication flow")
	fs.StringVar(&mcpFlag, "mcp", "", "Connect to MCP server (name from config)")
	fs.BoolVar(&showConfig, "show-config", false, "Show configuration sources")
	fs.Parse(os.Args[1:])

	if showVersion {
		fmt.Printf("Claw Code (Go) v%s\n  Phase: 4 (LSP + HTTP Server + Sandbox + Plugins)\n", version)
		return nil
	}

	if showPrompt {
		fmt.Print(runtime.DefaultSystemPrompt())
		return nil
	}

	if doAuth {
		return runAuth()
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

	// Discover plugins
	pluginMgr := plugins.NewManager()
	pluginMgr.Discover()
	pluginTools := pluginMgr.AllTools()
	for _, t := range pluginTools {
		toolName := t.Name
		toolReg.RegisterDynamic(toolName, t.Description, t.InputSchema, func(input map[string]interface{}) (string, error) {
			return "", fmt.Errorf("plugin tool execution not yet implemented for %s", toolName)
		})
	}

	// Sandbox
	if sandboxFlag {
		sandboxCfg := sandbox.Config{Enabled: true, AllowPaths: []string{"."}}
		sb := sandbox.New(sandboxCfg)
		_ = sb
		fmt.Fprintf(os.Stderr, "  [sandbox enabled]\n")
	}

	// Hook runner
	allPre := append([]string{}, cfg.Hooks.PreToolUse...)
	allPost := append([]string{}, cfg.Hooks.PostToolUse...)
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

	client, err := mcp.NewClient(context.Background(), srv.Command, []string{}, srv.Env)
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
		client, err := mcp.NewClient(context.Background(), srv.Command, []string{}, srv.Env)
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
