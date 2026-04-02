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

	"github.com/go-claw/claw/internal/api"
	"github.com/go-claw/claw/internal/commands"
	"github.com/go-claw/claw/internal/config"
	"github.com/go-claw/claw/internal/runtime"
	"github.com/go-claw/claw/internal/tools"
)

const version = "0.2.0"

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
		showVersion  bool
		showPrompt   bool
		showAgents   bool
		showSkills   bool
		showConfig   bool
	)

	fs := flag.NewFlagSet("claw", flag.ExitOnError)
	fs.StringVar(&modelFlag, "model", "", "Override the active model")
	fs.StringVar(&permFlag, "permission-mode", "", "Permission mode: read-only, workspace-write, danger-full-access")
	fs.StringVar(&outputFormat, "output-format", "text", "Non-interactive output format: text or json")
	fs.BoolVar(&showVersion, "version", false, "Print version")
	fs.BoolVar(&showPrompt, "system-prompt", false, "Print system prompt")
	fs.BoolVar(&showAgents, "agents", false, "List agents")
	fs.BoolVar(&showSkills, "skills", false, "List skills")
	fs.BoolVar(&showConfig, "show-config", false, "Show configuration sources")
	fs.Parse(os.Args[1:])

	if showVersion {
		fmt.Printf("Claw Code (Go)\n  Version: %s\n  Go: %s\n  OS: %s/%s\n", version, "1.23", os.Getenv("GOOS"), os.Getenv("GOARCH"))
		return nil
	}

	if showPrompt {
		fmt.Print(runtime.DefaultSystemPrompt())
		return nil
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

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve model
	model := resolveArg(modelFlag, "ANTHROPIC_MODEL", cfg.Model)
	model = api.ResolveModelAlias(model)

	// Resolve auth
	auth, err := api.ResolveAuth()
	if err != nil {
		return err
	}

	baseURL := api.BaseURL()

	// Permission mode
	permMode := resolveArg(permFlag, "CLAW_PERMISSION_MODE", cfg.PermissionMode)
	_ = permMode // used by runtime policy

	// Create provider
	provider := api.NewProvider(model, auth, baseURL)

	// Create tools
	toolReg := tools.NewToolRegistry()

	// Create hook runner from config
	hookRunner := runtime.NewHookRunner(cfg.Hooks.PreToolUse, cfg.Hooks.PostToolUse)
	_ = hookRunner // will be integrated into runtime

	// Create runtime
	rt := runtime.NewConversationRuntime(provider, toolReg, model)

	// Determine prompt from args
	args := fs.Args()
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		return runOnce(rt, prompt, outputFormat)
	}

	// Interactive REPL
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
			// Handle compact command specially
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

		// Check if compaction is needed
		if rt.ShouldCompact(compactionCfg) {
			rt.Compact(compactionCfg)
			fmt.Fprintf(os.Stderr, "  [auto-compacted, messages: %d]\n", rt.MessageCount())
		}

		// Run conversation turn
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

func resolveArg(flag string, env string, def string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}
