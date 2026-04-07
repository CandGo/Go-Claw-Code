package api

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// AuthMethod represents the authentication method used for API calls.
type AuthMethod int

const (
	AuthAPIKey    AuthMethod = iota // Standard API key (x-api-key header)
	AuthBearer                      // Bearer token (Authorization header)
	AuthAWSSigV4                    // AWS Bedrock SigV4 signing
	AuthGoogleADC                   // Google Vertex AI ADC token
	AuthAzureToken                  // Azure Foundry token
)

// AuthSource represents the authentication method for API calls.
type AuthSource struct {
	APIKey      string
	BearerToken string
	Method      AuthMethod
	// Cloud provider fields
	AWSCreds    *AWSCredentials
	AWSRegion   string
	GoogleToken string
	AzureToken  string
}

// HasCredentials returns true if any credentials are present.
func (a *AuthSource) HasCredentials() bool {
	if a.APIKey != "" || a.BearerToken != "" {
		return true
	}
	return a.Method == AuthAWSSigV4 || a.Method == AuthGoogleADC || a.Method == AuthAzureToken
}

// DetectProviderFromEnv checks environment variables to determine which
// cloud provider to use. Returns AuthAPIKey (default) if no cloud provider is detected.
func DetectProviderFromEnv() AuthMethod {
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_USE_BEDROCK")) {
		return AuthAWSSigV4
	}
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_USE_VERTEX")) {
		return AuthGoogleADC
	}
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_USE_FOUNDRY")) {
		return AuthAzureToken
	}
	return AuthAPIKey
}

// ResolveAuthForProvider resolves credentials for a specific cloud provider.
func ResolveAuthForProvider(method AuthMethod, model string) (*AuthSource, error) {
	ctx := context.Background()

	switch method {
	case AuthAWSSigV4:
		return resolveAWSAuth(ctx, model)
	case AuthGoogleADC:
		return resolveGoogleAuth(ctx, model)
	case AuthAzureToken:
		return resolveAzureAuth(ctx)
	default:
		return nil, fmt.Errorf("unsupported auth method: %d", method)
	}
}

// BaseURLForProvider returns the appropriate base URL for a cloud provider.
func BaseURLForProvider(method AuthMethod, model string) string {
	switch method {
	case AuthAWSSigV4:
		region := getAWSRegion()
		if model != "" {
			if envRegion := os.Getenv("ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION"); envRegion != "" {
				region = envRegion
			}
		}
		return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)

	case AuthGoogleADC:
		region := getVertexRegionForModel(model)
		projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
		return fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic",
			region, projectID, region,
		)

	case AuthAzureToken:
		if baseURL := os.Getenv("ANTHROPIC_FOUNDRY_BASE_URL"); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
		if resource := os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE"); resource != "" {
			return fmt.Sprintf("https://%s.services.ai.azure.com/anthropic/v1", resource)
		}
		return ""

	default:
		return BaseURL()
	}
}

func resolveAWSAuth(ctx context.Context, model string) (*AuthSource, error) {
	auth := &AuthSource{Method: AuthAWSSigV4}
	auth.AWSRegion = getAWSRegion()

	// Bearer token auth (Bedrock API Key)
	if bearerToken := os.Getenv("AWS_BEARER_TOKEN_BEDROCK"); bearerToken != "" {
		auth.BearerToken = bearerToken
		return auth, nil
	}

	// Skip auth entirely
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_SKIP_BEDROCK_AUTH")) {
		return auth, nil
	}

	// Get credentials via auth manager
	mgr := GetAWSAuthManager()
	creds, err := mgr.RefreshAndGetAWSCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get AWS credentials: %w", err)
	}
	auth.AWSCreds = creds
	return auth, nil
}

func resolveGoogleAuth(ctx context.Context, model string) (*AuthSource, error) {
	auth := &AuthSource{Method: AuthGoogleADC}

	if isEnvTruthy(os.Getenv("CLAUDE_CODE_SKIP_VERTEX_AUTH")) {
		return auth, nil
	}

	mgr := GetGoogleAuthManager()
	token, err := mgr.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Google credentials: %w", err)
	}
	auth.GoogleToken = token
	return auth, nil
}

func resolveAzureAuth(ctx context.Context) (*AuthSource, error) {
	auth := &AuthSource{Method: AuthAzureToken}

	// API key auth
	if apiKey := os.Getenv("ANTHROPIC_FOUNDRY_API_KEY"); apiKey != "" {
		auth.APIKey = apiKey
		return auth, nil
	}

	if isEnvTruthy(os.Getenv("CLAUDE_CODE_SKIP_FOUNDRY_AUTH")) {
		return auth, nil
	}

	mgr := GetAzureAuthManager()
	token, err := mgr.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Azure credentials: %w", err)
	}
	auth.AzureToken = token
	return auth, nil
}

// ResolveAuth discovers authentication credentials from environment variables.
// Checks CLAW_API_KEY first, then ANTHROPIC_API_KEY as fallback.
func ResolveAuth() (*AuthSource, error) {
	auth := &AuthSource{
		APIKey:      strings.TrimSpace(os.Getenv("CLAW_API_KEY")),
		BearerToken: strings.TrimSpace(os.Getenv("CLAW_AUTH_TOKEN")),
	}
	if !auth.HasCredentials() {
		// Fallback to ANTHROPIC_* env vars
		auth = &AuthSource{
			APIKey:      strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
			BearerToken: strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")),
		}
	}
	if !auth.HasCredentials() {
		return nil, MissingCredentials("Claw", []string{
			"CLAW_API_KEY",
			"ANTHROPIC_API_KEY",
		})
	}
	return auth, nil
}

// BaseURL returns the API base URL from environment or default.
// Checks CLAW_BASE_URL first, then ANTHROPIC_BASE_URL as fallback.
func BaseURL() string {
	if u := strings.TrimSpace(os.Getenv("CLAW_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.anthropic.com"
}
