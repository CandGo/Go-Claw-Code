package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// WizardResult holds the outcome of the setup wizard.
type WizardResult struct {
	APIKey      string
	BaseURL     string
	OAuthToken  bool
	Model       string
	UseClaudeCC bool // true = reuse existing Claude Code config
}

// IsTerminal returns true if stdin is connected to a terminal.
func IsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// detectClaudeCodeConfig checks if Claude Code (Rust) configuration exists.
func detectClaudeCodeConfig() (hasAPIKey, hasBaseURL, hasOAuth bool) {
	// Check env vars
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		hasAPIKey = true
	}
	if os.Getenv("ANTHROPIC_BASE_URL") != "" {
		hasBaseURL = true
	}

	// Check ~/.claude/credentials.json or ~/.claude/auth.json
	home, err := os.UserHomeDir()
	if err == nil {
		if data, err := os.ReadFile(home + "/.claude/auth.json"); err == nil && len(data) > 0 {
			hasAPIKey = true
		}
		if data, err := os.ReadFile(home + "/.claude/oauth_token.json"); err == nil && len(data) > 0 {
			hasOAuth = true
		}
	}
	return
}

// RunSetupWizard executes the interactive first-run setup wizard.
// The caller should check IsTerminal() before calling.
func RunSetupWizard(ver string) (*WizardResult, error) {
	result := &WizardResult{}

	// Welcome banner
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────┐")
	fmt.Printf("  │        \x1b[1mWelcome to Go-Claw-Code v%s\x1b[0m          │\n", ver)
	fmt.Println("  │                                                 │")
	fmt.Println("  │     Let's set up your credentials to start.      │")
	fmt.Println("  └─────────────────────────────────────────────────┘")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Check if Claude Code config exists
	hasAPIKey, hasBaseURL, hasOAuth := detectClaudeCodeConfig()
	hasClaudeCC := hasAPIKey || hasBaseURL || hasOAuth

	// Step 1: Choose auth method
	fmt.Println("  \x1b[1mChoose your authentication method:\x1b[0m")
	if hasClaudeCC {
		fmt.Println("    1. \x1b[36mUse existing Claude Code config\x1b[0m (reuse ANTHROPIC_API_KEY)")
		fmt.Println("    2. Enter a new API Key")
		fmt.Println("    3. OAuth login (browser-based)")
		fmt.Println("    4. Configure custom endpoint (Zhipu, DeepSeek, OpenAI, etc.)")
		fmt.Println("    5. Skip (use environment variables)")
		fmt.Println()

		choice := promptChoice(reader, "  Your choice [1-5]: ", []string{"1", "2", "3", "4", "5"})

		switch choice {
		case "1":
			result.UseClaudeCC = true
			fmt.Println()
			fmt.Println("  \x1b[32m✓ Will reuse Claude Code configuration.\x1b[0m")
			fmt.Println("    Go-Claw-Code will read ANTHROPIC_API_KEY / ANTHROPIC_BASE_URL from")
			fmt.Println("    your existing Claude Code setup. No separate config needed.")
			fmt.Println()
		case "2":
			if err := wizardAPIKey(reader, result); err != nil {
				return nil, err
			}
		case "3":
			if err := wizardOAuth(result); err != nil {
				return nil, err
			}
		case "4":
			if err := wizardCustomEndpoint(reader, result); err != nil {
				return nil, err
			}
		case "5":
			fmt.Println("  Skipped. Set CLAW_API_KEY or ANTHROPIC_API_KEY before running.")
			return result, nil
		}
	} else {
		fmt.Println("    1. Enter API Key")
		fmt.Println("    2. OAuth login (browser-based)")
		fmt.Println("    3. Configure custom endpoint (Zhipu, DeepSeek, OpenAI, etc.)")
		fmt.Println("    4. Skip (use environment variables)")
		fmt.Println()

		choice := promptChoice(reader, "  Your choice [1-4]: ", []string{"1", "2", "3", "4"})

		switch choice {
		case "1":
			if err := wizardAPIKey(reader, result); err != nil {
				return nil, err
			}
		case "2":
			if err := wizardOAuth(result); err != nil {
				return nil, err
			}
		case "3":
			if err := wizardCustomEndpoint(reader, result); err != nil {
				return nil, err
			}
		case "4":
			fmt.Println("  Skipped. Set CLAW_API_KEY or ANTHROPIC_API_KEY before running.")
			return result, nil
		}
	}

	// Step 2: Model selection (skip if reusing Claude Code config)
	if !result.UseClaudeCC {
		wizardModel(reader, result)
	}

	// Persist model + base URL to credentials file
	if !result.UseClaudeCC && result.APIKey != "" {
		existingCreds, _ := LoadCredentials()
		if existingCreds == nil {
			existingCreds = &Credentials{}
		}
		if result.Model != "" {
			existingCreds.Model = result.Model
		}
		if result.BaseURL != "" {
			existingCreds.BaseURL = result.BaseURL
		}
		SaveCredentials(existingCreds)
	}

	// Done
	fmt.Println()
	fmt.Println("  \x1b[32m✓ Setup complete!\x1b[0m")
	if result.UseClaudeCC {
		fmt.Println("    Auth: Reusing Claude Code config (ANTHROPIC_*)")
	} else if result.APIKey != "" {
		fmt.Println("    Auth: API Key (saved to ~/.go-claw/auth.json)")
	} else if result.OAuthToken {
		fmt.Println("    Auth: OAuth")
	}
	if result.Model != "" {
		fmt.Printf("    Model: %s\n", result.Model)
	}
	fmt.Println()

	return result, nil
}

// wizardAPIKey handles the "Enter API Key" flow.
func wizardAPIKey(reader *bufio.Reader, result *WizardResult) error {
	fmt.Println()
	fmt.Print("  Enter your API key: ")
	key, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	if err := SaveCredentials(&Credentials{APIKey: key}); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	result.APIKey = key
	path, _ := CredentialsFilePath()
	fmt.Printf("  \x1b[32mAPI key saved to %s\x1b[0m\n", path)
	return nil
}

// wizardOAuth handles the OAuth browser-based login flow.
func wizardOAuth(result *WizardResult) error {
	fmt.Println()
	fmt.Println("  Starting OAuth login flow...")

	cfg := DefaultOAuthConfig()
	token, err := StartAuthFlow(cfg)
	if err != nil {
		return fmt.Errorf("OAuth failed: %w", err)
	}
	if err := SaveToken(token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	result.OAuthToken = true
	fmt.Println("  \x1b[32mOAuth authentication successful!\x1b[0m")
	return nil
}

// wizardCustomEndpoint handles the custom endpoint flow.
func wizardCustomEndpoint(reader *bufio.Reader, result *WizardResult) error {
	fmt.Println()
	fmt.Print("  Enter the API base URL (e.g., https://open.bigmodel.cn/api/paas/v4): ")
	baseURL, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fmt.Errorf("base URL cannot be empty")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	fmt.Print("  Enter the API key for this endpoint: ")
	apiKey, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	if err := SaveCredentials(&Credentials{APIKey: apiKey, BaseURL: baseURL}); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	result.APIKey = apiKey
	result.BaseURL = baseURL
	path, _ := CredentialsFilePath()
	fmt.Printf("  \x1b[32mCredentials saved to %s\x1b[0m\n", path)
	return nil
}

// modelEntry describes a model with its required base URL.
type modelEntry struct {
	Name        string
	Description string
	BaseURL     string // empty = use Anthropic default
}

// knownModels lists available models with their required API endpoints.
var knownModels = []modelEntry{
	{"claude-sonnet-4-6", "recommended, best balance", ""},
	{"claude-opus-4-6", "most capable, higher cost", ""},
	{"claude-haiku-4-5", "fastest, lowest cost", ""},
	{"deepseek-chat", "DeepSeek", "https://api.deepseek.com"},
	{"glm-5.1", "Zhipu AI", "https://open.bigmodel.cn/api/paas/v4"},
	{"gpt-4o", "OpenAI", "https://api.openai.com/v1"},
}

// wizardModel handles optional model selection.
func wizardModel(reader *bufio.Reader, result *WizardResult) {
	fmt.Println()
	fmt.Println("  \x1b[1mChoose a model (optional):\x1b[0m")
	for i, m := range knownModels {
		fmt.Printf("    %d. %-22s (%s)\n", i+1, m.Name, m.Description)
	}
	fmt.Printf("    %d. Enter custom model name\n", len(knownModels)+1)
	fmt.Printf("    %d. Skip (use default)\n", len(knownModels)+2)
	fmt.Println()

	choices := make([]string, len(knownModels)+2)
	for i := range choices {
		choices[i] = fmt.Sprintf("%d", i+1)
	}
	choice := promptChoice(reader, fmt.Sprintf("  Your choice [1-%d]: ", len(choices)), choices)

	customIdx := len(knownModels) + 1
	skipIdx := len(knownModels) + 2

	if choice == fmt.Sprintf("%d", customIdx) {
		fmt.Print("  Model name: ")
		name, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		result.Model = strings.TrimSpace(name)
	} else if choice == fmt.Sprintf("%d", skipIdx) {
		// skip
	} else {
		idx := 0
		fmt.Sscanf(choice, "%d", &idx)
		if idx >= 1 && idx <= len(knownModels) {
			m := knownModels[idx-1]
			result.Model = m.Name
			// Auto-configure base URL for non-Claude models
			if m.BaseURL != "" && result.BaseURL == "" {
				fmt.Printf("  \x1b[36mNote: %s requires base URL: %s\x1b[0m\n", m.Name, m.BaseURL)
				fmt.Print("  Auto-configure this base URL? [Y/n]: ")
				ans, _ := reader.ReadString('\n')
				ans = strings.TrimSpace(strings.ToLower(ans))
				if ans == "" || ans == "y" || ans == "yes" {
					result.BaseURL = m.BaseURL
					// Update saved credentials with base URL
					if result.APIKey != "" {
						SaveCredentials(&Credentials{
							APIKey:  result.APIKey,
							BaseURL: result.BaseURL,
							Model:   result.Model,
		})
					}
					fmt.Printf("  \x1b[32mBase URL set to %s\x1b[0m\n", m.BaseURL)
				}
			}
		}
	}
}

// promptChoice displays a prompt and reads input, validating against allowed choices.
// Loops until a valid choice is entered.
func promptChoice(reader *bufio.Reader, prompt string, allowed []string) string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}

	for {
		fmt.Print(prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}
		input = strings.TrimSpace(input)
		if allowedSet[input] {
			return input
		}
		fmt.Printf("  \x1b[33mInvalid choice. Please enter one of: %s\x1b[0m\n", strings.Join(allowed, "/"))
	}
}
