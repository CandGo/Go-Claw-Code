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
	"github.com/go-claw/claw/internal/runtime"
	"github.com/go-claw/claw/internal/tools"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\nRun `claw --help` for usage.\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		modelFlag      string
		permFlag       string
		outputFormat   string
		showVersion    bool
		showPrompt     bool
		showAgents     bool
		showSkills     bool
	)

	fs := flag.NewFlagSet("claw", flag.ExitOnError)
	fs.StringVar(&modelFlag, "model", "", "Override the active model")
	fs.StringVar(&permFlag, "permission-mode", "danger-full-access", "Permission mode: read-only, workspace-write, danger-full-access")
	fs.StringVar(&outputFormat, "output-format", "text", "Non-interactive output format: text or json")
	fs.BoolVar(&showVersion, "version", false, "Print version")
	fs.BoolVar(&showPrompt, "system-prompt", false, "Print system prompt")
	fs.BoolVar(&showAgents, "agents", false, "List agents")
	fs.BoolVar(&showSkills, "skills", false, "List skills")
	fs.Parse(os.Args[1:])

	if showVersion {
		fmt.Printf("Claw Code (Go)\n  Version: %s\n  Built: %s\n", version, time.Now().Format("2006-03-31"))
		return nil
	}

	if showPrompt {
		fmt.Print(runtime.DefaultSystemPrompt())
		return nil
	}

	if showAgents {
		fmt.Println("No agents found.")
		return nil
	}

	if showSkills {
		fmt.Println("No skills found.")
		return nil
	}

	// Resolve model
	model := resolveArg(modelFlag, "ANTHROPIC_MODEL", "claude-sonnet-4-6")
	model = api.ResolveModelAlias(model)

	// Resolve auth
	auth, err := api.ResolveAuth()
	if err != nil {
		return err
	}

	baseURL := api.BaseURL()

	// Create provider
	provider := api.NewProvider(model, auth, baseURL)

	// Create tools
	toolReg := tools.NewToolRegistry()

	// Create runtime
	rt := runtime.NewConversationRuntime(provider, toolReg, model)

	// Determine prompt from args
	args := fs.Args()
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		return runOnce(rt, prompt, outputFormat)
	}

	// Interactive REPL
	return runREPL(rt)
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

func runREPL(rt *runtime.ConversationRuntime) error {
	fmt.Println("Claw Code (Go) v" + version)
	fmt.Printf("Model: %s\n", rt.Model())
	fmt.Println("Type /help for commands, Ctrl+D to exit")
	fmt.Println()

	rl, err := readline.New("> ")
	if err != nil {
		return fmt.Errorf("failed to init readline: %w", err)
	}
	defer rl.Close()

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
			if err := handleCommand(line, rt); err != nil {
				if err.Error() == "quit" {
					break
				}
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			continue
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

func handleCommand(cmd string, rt *runtime.ConversationRuntime) error {
	parts := strings.Fields(cmd)
	name := parts[0]

	switch name {
	case "/help":
		fmt.Println(`Slash commands:
  /help      Show this help
  /status    Show session status
  /model     Show current model
  /clear     Clear conversation
  /compact   Compact conversation history
  /cost      Show token usage
  /quit      Exit`)
	case "/status":
		fmt.Printf("Model: %s\nMessages: %d\n", rt.Model(), rt.MessageCount())
	case "/model":
		fmt.Println(rt.Model())
	case "/clear":
		rt.Clear()
		fmt.Println("Session cleared.")
	case "/compact":
		fmt.Println("Compaction not yet implemented.")
	case "/cost":
		fmt.Println("Cost tracking not yet implemented.")
	case "/quit", "/exit":
		return fmt.Errorf("quit")
	default:
		fmt.Printf("Unknown command: %s\n", name)
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
