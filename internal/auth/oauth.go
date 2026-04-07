package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultClientID    = "claw-code-go"
	defaultCallbackPort = 8765
	defaultAuthURL     = "https://auth.anthropic.com/authorize"
	defaultTokenURL    = "https://auth.anthropic.com/oauth/token"
)

// OAuthConfig holds OAuth PKCE configuration.
type OAuthConfig struct {
	ClientID          string `json:"client_id"`
	AuthURL           string `json:"auth_url"`
	TokenURL          string `json:"token_url"`
	CallbackPort      int    `json:"callback_port"`
	ManualRedirectURL string `json:"manual_redirect_url,omitempty"` // for non-browser auth flows
	Scopes            string `json:"scopes"`
}

// OAuthToken holds the OAuth token response.
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	ExpiresAtMs  int64  `json:"expires_at_ms,omitempty"` // Set after loading for expiry checks
}

// OAuthState holds PKCE state for an in-progress auth flow.
type OAuthState struct {
	Verifier  string
	Challenge string
	State     string
}

// DefaultOAuthConfig returns default OAuth config.
func DefaultOAuthConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     defaultClientID,
		AuthURL:      defaultAuthURL,
		TokenURL:     defaultTokenURL,
		CallbackPort: defaultCallbackPort,
		Scopes:       "openid profile email",
	}
}

// GeneratePKCE generates a PKCE code verifier and challenge.
func GeneratePKCE() (*OAuthState, error) {
	verifier := make([]byte, 32)
	if _, err := rand.Read(verifier); err != nil {
		return nil, fmt.Errorf("failed to generate verifier: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(verifier)
	hash := sha256.Sum256([]byte(encoded))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	state := make([]byte, 16)
	rand.Read(state)
	stateEncoded := base64.RawURLEncoding.EncodeToString(state)

	return &OAuthState{
		Verifier:  encoded,
		Challenge: challenge,
		State:     stateEncoded,
	}, nil
}

// StartAuthFlow starts the OAuth PKCE flow.
// It opens the browser and waits for the callback.
func StartAuthFlow(cfg OAuthConfig) (*OAuthToken, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	// Determine redirect URI
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", cfg.CallbackPort)

	// Build auth URL
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", cfg.Scopes)
	params.Set("state", pkce.State)
	params.Set("code_challenge", pkce.Challenge)
	params.Set("code_challenge_method", "S256")

	authURL := cfg.AuthURL + "?" + params.Encode()

	// Start callback server
	tokenCh := make(chan *OAuthToken, 1)
	errCh := make(chan error, 1)

	handler := http.NewServeMux()
	handler.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != pkce.State {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}

		// Exchange code for token
		token, err := exchangeCode(cfg, code, pkce.Verifier)
		if err != nil {
			errCh <- err
			http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "<html><body><h1>Success! You can close this tab.</h1></body></html>")
		tokenCh <- token
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.CallbackPort))
	if err != nil {
		// If ManualRedirectURL is set, fall back to manual auth flow
		if cfg.ManualRedirectURL != "" {
			// Rebuild auth URL with the manual redirect
			params.Set("redirect_uri", cfg.ManualRedirectURL)
			authURL = cfg.AuthURL + "?" + params.Encode()
			fmt.Printf("Callback server unavailable. Visit this URL to authenticate:\n\n%s\n\n", authURL)
			fmt.Print("Paste the authorization code: ")
			var code string
			if _, scanErr := fmt.Scanln(&code); scanErr != nil {
				return nil, fmt.Errorf("failed to read authorization code: %w", scanErr)
			}
			// Use manual redirect URI for token exchange
			redirectURI = cfg.ManualRedirectURL
			token, exchangeErr := exchangeCodeWithRedirect(cfg, code, pkce.Verifier, redirectURI)
			if exchangeErr != nil {
				return nil, exchangeErr
			}
			return token, nil
		}
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}

	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	defer server.Close()

	// Open browser
	fmt.Println("Opening browser for authentication...")
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser. Please visit:\n%s\n", authURL)
	}

	// Wait for token or error
	select {
	case token := <-tokenCh:
		return token, nil
	case err := <-errCh:
		return nil, err
	}
}

func exchangeCode(cfg OAuthConfig, code, verifier string) (*OAuthToken, error) {
	return exchangeCodeWithRedirect(cfg, code, verifier, fmt.Sprintf("http://127.0.0.1:%d/callback", cfg.CallbackPort))
}

// exchangeCodeWithRedirect exchanges an authorization code for a token using a custom redirect URI.
func exchangeCodeWithRedirect(cfg OAuthConfig, code, verifier, redirectURI string) (*OAuthToken, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("client_id", cfg.ClientID)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", verifier)

	resp, err := http.PostForm(cfg.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Compute absolute expiry from ExpiresIn
	token.ComputeExpiry()

	return &token, nil
}

// SaveToken persists the OAuth token to disk.
func SaveToken(token *OAuthToken) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".go-claw")
	os.MkdirAll(dir, 0700)

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "oauth_token.json"), data, 0600)
}

// LoadToken loads a persisted OAuth token.
func LoadToken() (*OAuthToken, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".go-claw", "oauth_token.json"))
	if err != nil {
		return nil, err
	}
	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Try common Linux browsers
		for _, browser := range []string{"xdg-open", "google-chrome", "firefox"} {
			if p, _ := exec.LookPath(browser); p != "" {
				cmd = exec.Command(p, url)
				break
			}
		}
	}
	if cmd == nil {
		return fmt.Errorf("no browser found")
	}
	return cmd.Start()
}

// ResolveAuthWithOAuth first tries API key, then falls back to OAuth token.
// Mirrors Rust resolve_startup_auth_source — includes automatic token refresh.
func ResolveAuthWithOAuth() (apiKey, bearerToken string, err error) {
	// 1. Try CLAW_* environment variables first (Go-Claw-Code specific)
	apiKey = strings.TrimSpace(os.Getenv("CLAW_API_KEY"))
	bearerToken = strings.TrimSpace(os.Getenv("CLAW_AUTH_TOKEN"))
	if apiKey != "" || bearerToken != "" {
		return apiKey, bearerToken, nil
	}

	// 2. Try file-based credentials (~/.go-claw/auth.json)
	creds, credErr := LoadCredentials()
	if credErr == nil && creds != nil && creds.APIKey != "" {
		if creds.APIKey == "(claude-code)" {
			// User chose to reuse Claude Code config — fall through to ANTHROPIC_* env vars
		} else {
			return creds.APIKey, "", nil
		}
	}

	// 3. Try ANTHROPIC_* environment variables (fallback, compatible with Claude Code)
	apiKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	bearerToken = strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if apiKey != "" || bearerToken != "" {
		return apiKey, bearerToken, nil
	}

	// Try OAuth token
	token, err := LoadToken()
	if err != nil || token.AccessToken == "" {
		return "", "", fmt.Errorf("no credentials found; set CLAW_API_KEY or ANTHROPIC_API_KEY, or run 'claw --login'")
	}

	// Check expiry and attempt refresh
	if token.IsExpired() {
		if token.RefreshToken != "" {
			refreshed, refreshErr := refreshToken(DefaultOAuthConfig(), token.RefreshToken)
			if refreshErr != nil {
				return "", "", fmt.Errorf("OAuth token expired and refresh failed: %w; run 'claw --login'", refreshErr)
			}
			// Save the refreshed token
			if saveErr := SaveToken(refreshed); saveErr != nil {
				// Non-fatal: we still have a valid token
				_ = saveErr
			}
			return "", refreshed.AccessToken, nil
		}
		return "", "", fmt.Errorf("OAuth token expired with no refresh token; run 'claw --login'")
	}

	return "", token.AccessToken, nil
}

// refreshToken refreshes an OAuth token using the refresh_token grant.
func refreshToken(cfg OAuthConfig, refreshTokenStr string) (*OAuthToken, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshTokenStr)
	data.Set("client_id", cfg.ClientID)

	resp, err := http.PostForm(cfg.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	// Preserve refresh token if server doesn't return a new one
	if token.RefreshToken == "" {
		token.RefreshToken = refreshTokenStr
	}

	token.ComputeExpiry()
	return &token, nil
}

// IsExpired checks if the OAuth token has expired.
// Mirrors Rust oauth_token_is_expired.
func (t *OAuthToken) IsExpired() bool {
	if t.ExpiresAtMs > 0 {
		return time.Now().UnixMilli() >= t.ExpiresAtMs
	}
	if t.ExpiresIn > 0 {
		// No absolute expiry stored; assume still valid (we can't check without knowing when it was issued)
		return false
	}
	return false
}

// ComputeExpiry computes ExpiresAtMs from ExpiresIn seconds if not already set.
func (t *OAuthToken) ComputeExpiry() {
	if t.ExpiresAtMs == 0 && t.ExpiresIn > 0 {
		t.ExpiresAtMs = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).UnixMilli()
	}
}

// HasAuthFromEnvOrSaved checks if any auth source is available.
// Mirrors Rust has_auth_from_env_or_saved.
func HasAuthFromEnvOrSaved() bool {
	_, _, err := ResolveAuthWithOAuth()
	return err == nil
}

// MaskedToken returns a masked version of a token for logging.
// Mirrors Rust masked_authorization_header.
func MaskedToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
