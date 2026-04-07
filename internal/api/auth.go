package api

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// AuthMethod enumerates supported authentication strategies.
type AuthMethod int

const (
	AuthAPIKey    AuthMethod = iota // Standard x-api-key header
	AuthBearer                      // Bearer token via Authorization header
	AuthAWSSigV4                    // AWS Bedrock SigV4 request signing
	AuthGoogleADC                   // Google Vertex AI via Application Default Credentials
	AuthAzureToken                  // Azure Foundry via Azure AD token
)

// AuthSource holds resolved credentials for any provider.
type AuthSource struct {
	APIKey      string
	BearerToken string
	Method      AuthMethod
	// Cloud provider credential fields
	AWSCreds    *BedrockCreds
	AWSRegion   string
	GoogleToken string
	AzureToken  string
}

// HasCredentials reports whether any credential data is present.
func (a *AuthSource) HasCredentials() bool {
	if a.APIKey != "" || a.BearerToken != "" {
		return true
	}
	return a.Method == AuthAWSSigV4 || a.Method == AuthGoogleADC || a.Method == AuthAzureToken
}

// envIsTruthy checks whether an env-var value represents an affirmative setting.
func envIsTruthy(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// DetectProviderFromEnv inspects standard environment variables to determine
// which cloud provider should handle API requests. Returns AuthAPIKey when
// no cloud provider override is detected.
func DetectProviderFromEnv() AuthMethod {
	if envIsTruthy(os.Getenv("CLAUDE_CODE_USE_BEDROCK")) {
		return AuthAWSSigV4
	}
	if envIsTruthy(os.Getenv("CLAUDE_CODE_USE_VERTEX")) {
		return AuthGoogleADC
	}
	if envIsTruthy(os.Getenv("CLAUDE_CODE_USE_FOUNDRY")) {
		return AuthAzureToken
	}
	return AuthAPIKey
}

// ResolveAuthForProvider obtains credentials for the detected cloud provider.
func ResolveAuthForProvider(method AuthMethod, model string) (*AuthSource, error) {
	ctx := context.Background()
	switch method {
	case AuthAWSSigV4:
		return resolveBedrockAuth(ctx, model)
	case AuthGoogleADC:
		return resolveVertexAuth(ctx)
	case AuthAzureToken:
		return resolveFoundryAuth(ctx)
	default:
		return nil, fmt.Errorf("unsupported auth method: %d", method)
	}
}

// BaseURLForProvider returns the API endpoint appropriate for the given provider.
func BaseURLForProvider(method AuthMethod, model string) string {
	switch method {
	case AuthAWSSigV4:
		r := awsRegion()
		if model != "" {
			if alt := os.Getenv("ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION"); alt != "" {
				r = alt
			}
		}
		return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", r)
	case AuthGoogleADC:
		r := vertexRegion(model)
		pid := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
		return fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic",
			r, pid, r,
		)
	case AuthAzureToken:
		if u := strings.TrimSpace(os.Getenv("ANTHROPIC_FOUNDRY_BASE_URL")); u != "" {
			return strings.TrimRight(u, "/")
		}
		if res := os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE"); res != "" {
			return fmt.Sprintf("https://%s.services.ai.azure.com/anthropic/v1", res)
		}
		return ""
	default:
		return BaseURL()
	}
}

// awsRegion resolves the preferred AWS region from the environment.
// Priority: AWS_REGION > AWS_DEFAULT_REGION > fallback "us-east-1".
func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}

// vertexRegion picks a Vertex AI region, optionally influenced by the model name.
func vertexRegion(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "haiku"):
		if r := os.Getenv("VERTEX_REGION_CLAUDE_3_5_HAIKU"); r != "" {
			return r
		}
		if r := os.Getenv("VERTEX_REGION_CLAUDE_HAIKU_4_5"); r != "" {
			return r
		}
	case strings.Contains(m, "sonnet"):
		if r := os.Getenv("VERTEX_REGION_CLAUDE_3_5_SONNET"); r != "" {
			return r
		}
		if r := os.Getenv("VERTEX_REGION_CLAUDE_3_7_SONNET"); r != "" {
			return r
		}
	}
	if r := os.Getenv("CLOUD_ML_REGION"); r != "" {
		return r
	}
	return "us-east5"
}

// ResolveAuth discovers authentication credentials from environment variables.
// Checks CLAW_API_KEY first, then ANTHROPIC_API_KEY as fallback.
func ResolveAuth() (*AuthSource, error) {
	auth := &AuthSource{
		APIKey:      strings.TrimSpace(os.Getenv("CLAW_API_KEY")),
		BearerToken: strings.TrimSpace(os.Getenv("CLAW_AUTH_TOKEN")),
	}
	if !auth.HasCredentials() {
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
func BaseURL() string {
	if u := strings.TrimSpace(os.Getenv("CLAW_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.anthropic.com"
}
