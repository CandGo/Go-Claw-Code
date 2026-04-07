package runtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goRuntime "runtime"
	"sort"
	"strings"
	"time"
)

// OAuthTokenSet holds the OAuth credentials.
// Mirrors Rust OAuthTokenSet.
type OAuthTokenSet struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken,omitempty"`
	ExpiresAt    uint64   `json:"expiresAt,omitempty"`
	Scopes       []string `json:"scopes"`
}

// PkceCodePair holds the PKCE verifier and challenge.
type PkceCodePair struct {
	Verifier         string
	Challenge        string
	ChallengeMethod  string // always "S256"
}

// OAuthAuthorizationRequest represents an authorization URL request.
type OAuthAuthorizationRequest struct {
	AuthorizeURL       string
	ClientID           string
	RedirectURI        string
	Scopes             []string
	State              string
	CodeChallenge      string
	CodeChallengeMethod string
	ExtraParams        map[string]string
}

// OAuthTokenExchangeRequest holds the token exchange parameters.
type OAuthTokenExchangeRequest struct {
	GrantType   string
	Code        string
	RedirectURI string
	ClientID    string
	CodeVerifier string
	State       string
}

// OAuthRefreshRequest holds the refresh token parameters.
type OAuthRefreshRequest struct {
	GrantType    string
	RefreshToken string
	ClientID     string
	Scopes       []string
}

// OAuthCallbackParams holds parsed callback query parameters.
type OAuthCallbackParams struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// OAuthConfig holds OAuth configuration.
type OAuthConfig struct {
	ClientID           string   `json:"client_id"`
	AuthorizeURL       string   `json:"authorize_url"`
	TokenURL           string   `json:"token_url"`
	CallbackPort       int      `json:"callback_port,omitempty"`
	ManualRedirectURL  string   `json:"manual_redirect_url,omitempty"`
	Scopes             []string `json:"scopes"`
}

// NewOAuthAuthorizationRequest creates an authorization request from config.
func NewOAuthAuthorizationRequest(config OAuthConfig, redirectURI, state string, pkce *PkceCodePair) OAuthAuthorizationRequest {
	return OAuthAuthorizationRequest{
		AuthorizeURL:        config.AuthorizeURL,
		ClientID:            config.ClientID,
		RedirectURI:         redirectURI,
		Scopes:              config.Scopes,
		State:               state,
		CodeChallenge:       pkce.Challenge,
		CodeChallengeMethod: pkce.ChallengeMethod,
		ExtraParams:         make(map[string]string),
	}
}

// WithExtraParam adds an extra parameter (builder pattern).
func (r OAuthAuthorizationRequest) WithExtraParam(key, value string) OAuthAuthorizationRequest {
	if r.ExtraParams == nil {
		r.ExtraParams = make(map[string]string)
	}
	r.ExtraParams[key] = value
	return r
}

// BuildURL constructs the full authorization URL.
func (r OAuthAuthorizationRequest) BuildURL() string {
	params := [][2]string{
		{"response_type", "code"},
		{"client_id", r.ClientID},
		{"redirect_uri", r.RedirectURI},
		{"scope", strings.Join(r.Scopes, " ")},
		{"state", r.State},
		{"code_challenge", r.CodeChallenge},
		{"code_challenge_method", r.CodeChallengeMethod},
	}
	for k, v := range r.ExtraParams {
		params = append(params, [2]string{k, v})
	}
	query := make([]string, 0, len(params))
	for _, p := range params {
		query = append(query, percentEncode(p[0])+"="+percentEncode(p[1]))
	}
	sep := "?"
	if strings.Contains(r.AuthorizeURL, "?") {
		sep = "&"
	}
	return r.AuthorizeURL + sep + strings.Join(query, "&")
}

// NewOAuthTokenExchangeRequest creates a token exchange request from config.
func NewOAuthTokenExchangeRequest(config OAuthConfig, code, state, verifier, redirectURI string) OAuthTokenExchangeRequest {
	return OAuthTokenExchangeRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  redirectURI,
		ClientID:     config.ClientID,
		CodeVerifier: verifier,
		State:        state,
	}
}

// FormParams returns the form parameters for token exchange.
func (r OAuthTokenExchangeRequest) FormParams() map[string]string {
	return map[string]string{
		"grant_type":    r.GrantType,
		"code":          r.Code,
		"redirect_uri":  r.RedirectURI,
		"client_id":     r.ClientID,
		"code_verifier": r.CodeVerifier,
		"state":         r.State,
	}
}

// NewOAuthRefreshRequest creates a refresh request from config.
func NewOAuthRefreshRequest(config OAuthConfig, refreshToken string, scopes []string) OAuthRefreshRequest {
	if scopes == nil {
		scopes = config.Scopes
	}
	return OAuthRefreshRequest{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
		ClientID:     config.ClientID,
		Scopes:       scopes,
	}
}

// FormParams returns the form parameters for token refresh.
func (r OAuthRefreshRequest) FormParams() map[string]string {
	return map[string]string{
		"grant_type":    r.GrantType,
		"refresh_token": r.RefreshToken,
		"client_id":     r.ClientID,
		"scope":         strings.Join(r.Scopes, " "),
	}
}

// GeneratePkcePair generates a PKCE code verifier and challenge pair.
func GeneratePkcePair() (*PkceCodePair, error) {
	verifier, err := generateRandomToken(32)
	if err != nil {
		return nil, err
	}
	return &PkceCodePair{
		Verifier:        verifier,
		Challenge:       CodeChallengeS256(verifier),
		ChallengeMethod: "S256",
	}, nil
}

// GenerateState generates a random OAuth state parameter.
func GenerateState() (string, error) {
	return generateRandomToken(32)
}

// CodeChallengeS256 computes the S256 PKCE code challenge.
func CodeChallengeS256(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64urlEncode(digest[:])
}

// LoopbackRedirectURI builds the loopback redirect URI.
func LoopbackRedirectURI(port int) string {
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

// CredentialsPath returns the path to the credentials file.
func CredentialsPath() (string, error) {
	dir, err := credentialsHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// LoadOAuthCredentials loads saved OAuth credentials from disk.
func LoadOAuthCredentials() (*OAuthTokenSet, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	root, err := readCredentialsRoot(path)
	if err != nil {
		return nil, err
	}
	oauthRaw, ok := root["oauth"]
	if !ok {
		return nil, nil
	}
	oauthMap, ok := oauthRaw.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	data, _ := json.Marshal(oauthMap)
	var tokenSet OAuthTokenSet
	if err := json.Unmarshal(data, &tokenSet); err != nil {
		return nil, fmt.Errorf("invalid oauth credentials: %w", err)
	}
	return &tokenSet, nil
}

// SaveOAuthCredentials persists OAuth credentials to disk.
func SaveOAuthCredentials(tokenSet *OAuthTokenSet) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	root, err := readCredentialsRoot(path)
	if err != nil {
		return err
	}
	data, err := json.Marshal(tokenSet)
	if err != nil {
		return fmt.Errorf("marshal oauth credentials: %w", err)
	}
	var oauthVal interface{}
	json.Unmarshal(data, &oauthVal)
	root["oauth"] = oauthVal
	return writeCredentialsRoot(path, root)
}

// ClearOAuthCredentials removes saved OAuth credentials.
func ClearOAuthCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	root, err := readCredentialsRoot(path)
	if err != nil {
		return err
	}
	delete(root, "oauth")
	return writeCredentialsRoot(path, root)
}

// OAuthTokenIsExpired checks if the token set is expired.
// Mirrors Rust oauth_token_is_expired.
func OAuthTokenIsExpired(tokens *OAuthTokenSet) bool {
	if tokens == nil || tokens.ExpiresAt == 0 {
		return true
	}
	now := uint64(time.Now().Unix())
	return now >= tokens.ExpiresAt
}

// ResolveSavedOAuthToken loads saved OAuth credentials and returns the access token.
// Mirrors Rust resolve_saved_oauth_token_set — includes auto-refresh.
func ResolveSavedOAuthToken() (string, error) {
	tokens, err := LoadOAuthCredentials()
	if err != nil || tokens == nil {
		return "", fmt.Errorf("no saved OAuth token")
	}
	if OAuthTokenIsExpired(tokens) {
		if tokens.RefreshToken == "" {
			return "", fmt.Errorf("OAuth token expired with no refresh token")
		}
		// Attempt refresh using default config
		cfg := DefaultRuntimeOAuthConfig()
		refreshReq := NewOAuthRefreshRequest(cfg, tokens.RefreshToken, nil)
		refreshed, refreshErr := RefreshOAuthToken(cfg.TokenURL, refreshReq)
		if refreshErr != nil {
			return "", fmt.Errorf("OAuth token refresh failed: %w", refreshErr)
		}
		// Preserve refresh token if server doesn't return one
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = tokens.RefreshToken
		}
		// Save refreshed credentials
		if saveErr := SaveOAuthCredentials(refreshed); saveErr != nil {
			_ = saveErr // Non-fatal
		}
		return refreshed.AccessToken, nil
	}
	return tokens.AccessToken, nil
}

// DefaultRuntimeOAuthConfig returns the default OAuth config matching the Rust reference.
func DefaultRuntimeOAuthConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		AuthorizeURL: "https://platform.claw.dev/oauth/authorize",
		TokenURL:     "https://platform.claw.dev/v1/oauth/token",
	}
}

// ParseOAuthCallbackRequestTarget parses an HTTP request target for OAuth callback.
func ParseOAuthCallbackRequestTarget(target string) (*OAuthCallbackParams, error) {
	path, query := target, ""
	if idx := strings.IndexByte(target, '?'); idx >= 0 {
		path = target[:idx]
		query = target[idx+1:]
	}
	if path != "/callback" {
		return nil, fmt.Errorf("unexpected callback path: %s", path)
	}
	return ParseOAuthCallbackQuery(query)
}

// ParseOAuthCallbackQuery parses OAuth callback query parameters.
func ParseOAuthCallbackQuery(query string) (*OAuthCallbackParams, error) {
	params := make(map[string]string)
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		key, value := pair, ""
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			key = pair[:idx]
			value = pair[idx+1:]
		}
		decodedKey, err := percentDecode(key)
		if err != nil {
			return nil, err
		}
		decodedValue, err := percentDecode(value)
		if err != nil {
			return nil, err
		}
		params[decodedKey] = decodedValue
	}
	return &OAuthCallbackParams{
		Code:             params["code"],
		State:            params["state"],
		Error:            params["error"],
		ErrorDescription: params["error_description"],
	}, nil
}

// ExchangeOAuthToken exchanges an authorization code for tokens.
func ExchangeOAuthToken(tokenURL string, req OAuthTokenExchangeRequest) (*OAuthTokenSet, error) {
	form := url.Values{}
	for k, v := range req.FormParams() {
		form.Set(k, v)
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    int      `json:"expires_in"`
		Scope        string   `json:"scope"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	scopes := strings.Fields(result.Scope)
	tokenSet := &OAuthTokenSet{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Scopes:       scopes,
	}
	// Compute ExpiresAt from ExpiresIn
	if result.ExpiresIn > 0 {
		tokenSet.ExpiresAt = uint64(time.Now().Unix()) + uint64(result.ExpiresIn)
	}
	return tokenSet, nil
}

// RefreshOAuthToken refreshes an access token.
func RefreshOAuthToken(tokenURL string, req OAuthRefreshRequest) (*OAuthTokenSet, error) {
	form := url.Values{}
	for k, v := range req.FormParams() {
		form.Set(k, v)
	}
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	tokenSet := &OAuthTokenSet{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Scopes:       strings.Fields(result.Scope),
	}
	if result.ExpiresIn > 0 {
		tokenSet.ExpiresAt = uint64(time.Now().Unix()) + uint64(result.ExpiresIn)
	}
	return tokenSet, nil
}

// StartCallbackServer starts a loopback HTTP server to receive the OAuth callback.
// Returns the redirect URI and a channel that receives the parsed callback params.
// The caller should open the auth URL in a browser, then wait on the channel.
func StartCallbackServer(port int) (string, <-chan *OAuthCallbackParams, error) {
	ch := make(chan *OAuthCallbackParams, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		params, err := ParseOAuthCallbackQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "Invalid callback: "+err.Error(), http.StatusBadRequest)
			ch <- &OAuthCallbackParams{Error: "parse_error", ErrorDescription: err.Error()}
			return
		}
		// Show success page to user
		w.Header().Set("Content-Type", "text/html")
		if params.Error != "" {
			fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2><p>%s: %s</p><p>You can close this tab.</p></body></html>", params.Error, params.ErrorDescription)
		} else {
			fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>")
		}
		ch <- params
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			ch <- &OAuthCallbackParams{Error: "server_error", ErrorDescription: err.Error()}
		}
	}()

	redirectURI := LoopbackRedirectURI(port)
	return redirectURI, ch, nil
}

// OpenBrowser opens the default browser to the given URL.
func OpenBrowser(url string) error {
	var cmd string
	var args []string
	switch goRuntime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, append(args, url)...).Start()
}

// Internal helpers

func generateRandomToken(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return base64urlEncode(buf), nil
}

func credentialsHomeDir() (string, error) {
	if path := os.Getenv("CLAW_CONFIG_HOME"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("HOME not set: %w", err)
	}
	return filepath.Join(home, ".go-claw"), nil
}

func readCredentialsRoot(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return make(map[string]interface{}), nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("credentials file must contain a JSON object: %w", err)
	}
	return root, nil
}

func writeCredentialsRoot(path string, root map[string]interface{}) error {
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func base64urlEncode(bytes []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out strings.Builder
	i := 0
	for i+3 <= len(bytes) {
		block := uint32(bytes[i])<<16 | uint32(bytes[i+1])<<8 | uint32(bytes[i+2])
		out.WriteByte(table[(block>>18)&0x3F])
		out.WriteByte(table[(block>>12)&0x3F])
		out.WriteByte(table[(block>>6)&0x3F])
		out.WriteByte(table[block&0x3F])
		i += 3
	}
	switch len(bytes) - i {
	case 1:
		block := uint32(bytes[i]) << 16
		out.WriteByte(table[(block>>18)&0x3F])
		out.WriteByte(table[(block>>12)&0x3F])
	case 2:
		block := uint32(bytes[i])<<16 | uint32(bytes[i+1])<<8
		out.WriteByte(table[(block>>18)&0x3F])
		out.WriteByte(table[(block>>12)&0x3F])
		out.WriteByte(table[(block>>6)&0x3F])
	}
	return out.String()
}

func percentEncode(value string) string {
	var encoded strings.Builder
	for _, b := range []byte(value) {
		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9',
			b == '-', b == '_', b == '.', b == '~':
			encoded.WriteByte(b)
		default:
			fmt.Fprintf(&encoded, "%%%02X", b)
		}
	}
	return encoded.String()
}

func percentDecode(value string) (string, error) {
	var decoded []byte
	bytes := []byte(value)
	for i := 0; i < len(bytes); i++ {
		switch bytes[i] {
		case '%':
			if i+2 >= len(bytes) {
				return "", fmt.Errorf("invalid percent-encoding")
			}
			hi, err := decodeHex(bytes[i+1])
			if err != nil {
				return "", err
			}
			lo, err := decodeHex(bytes[i+2])
			if err != nil {
				return "", err
			}
			decoded = append(decoded, (hi<<4)|lo)
			i += 2
		case '+':
			decoded = append(decoded, ' ')
		default:
			decoded = append(decoded, bytes[i])
		}
	}
	return string(decoded), nil
}

func decodeHex(b byte) (byte, error) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', nil
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, nil
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid percent-encoding byte: %c", b)
	}
}

// Sort helper for deterministic output
var _ = sort.Strings
