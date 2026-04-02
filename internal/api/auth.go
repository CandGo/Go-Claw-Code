package api

import (
	"os"
	"strings"
)

// AuthSource represents the authentication method for API calls.
type AuthSource struct {
	APIKey      string
	BearerToken string
}

// HasCredentials returns true if any credentials are present.
func (a *AuthSource) HasCredentials() bool {
	return a.APIKey != "" || a.BearerToken != ""
}

// ResolveAuth discovers authentication credentials from environment variables.
// Checks ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN.
func ResolveAuth() (*AuthSource, error) {
	auth := &AuthSource{
		APIKey:      strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		BearerToken: strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")),
	}
	if !auth.HasCredentials() {
		return nil, MissingCredentials("Claw", []string{
			"ANTHROPIC_API_KEY",
			"ANTHROPIC_AUTH_TOKEN",
		})
	}
	return auth, nil
}

// BaseURL returns the API base URL from environment or default.
func BaseURL() string {
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.anthropic.com"
}
