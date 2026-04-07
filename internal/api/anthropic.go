package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	anthropicVersion      = "2023-06-01"
	defaultBaseURL        = "https://api.anthropic.com"
	defaultInitialBackoff = 200 * time.Millisecond
	defaultMaxBackoff     = 2 * time.Second
	defaultMaxRetries     = 2
)

// --- OAuth token persistence (mirrors Rust oauth_credentials) ---

// OAuthCredentials holds saved OAuth tokens.
// Mirrors Rust OAuthCredentials.
type OAuthCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type,omitempty"`
}

// IsExpired returns true if the token has expired.
func (c *OAuthCredentials) IsExpired() bool {
	return time.Now().Unix() >= c.ExpiresAt
}

// oAuthCredentialsPath returns the path for saved OAuth credentials.
func oAuthCredentialsPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".go-claw", "oauth_credentials.json")
	}
	return ""
}

// LoadOAuthCredentials loads saved OAuth credentials from disk.
// Mirrors Rust load_saved_oauth_token.
func LoadOAuthCredentials() (*OAuthCredentials, error) {
	path := oAuthCredentialsPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var creds OAuthCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// SaveOAuthCredentials persists OAuth credentials to disk.
// Mirrors Rust save_oauth_credentials.
func SaveOAuthCredentials(creds *OAuthCredentials) error {
	path := oAuthCredentialsPath()
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// RefreshOAuthToken refreshes an OAuth token using the refresh_token grant.
// Mirrors Rust refresh_oauth_token.
func RefreshOAuthToken(tokenURL, refreshToken string) (*OAuthCredentials, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OAuth refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	creds := &OAuthCredentials{
		AccessToken: result.AccessToken,
		ExpiresAt:   time.Now().Unix() + int64(result.ExpiresIn),
		TokenType:   result.TokenType,
	}
	// Preserve old refresh token if the response doesn't include a new one
	if result.RefreshToken != "" {
		creds.RefreshToken = result.RefreshToken
	} else {
		creds.RefreshToken = refreshToken
	}

	return creds, nil
}

// --- Auth resolution (mirrors Rust resolve_startup_auth_source) ---

// ResolveAuthWithFallback resolves authentication, falling back to saved OAuth tokens.
// Mirrors Rust from_env_or_saved + resolve_startup_auth_source.
func ResolveAuthWithFallback(oauthTokenURL string) (*AuthSource, error) {
	// 1. Try CLAW_* environment variables first
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
	if auth.HasCredentials() {
		return auth, nil
	}

	// 2. Try saved OAuth credentials
	creds, err := LoadOAuthCredentials()
	if err != nil || creds == nil {
		return nil, MissingCredentials("Claw", []string{
			"ANTHROPIC_API_KEY",
			"ANTHROPIC_AUTH_TOKEN",
		})
	}

	// 3. If not expired, use the saved access token
	if !creds.IsExpired() {
		auth.BearerToken = creds.AccessToken
		return auth, nil
	}

	// 4. Try refreshing the token
	if creds.RefreshToken == "" || oauthTokenURL == "" {
		return nil, NewExpiredOAuthToken()
	}

	refreshed, err := RefreshOAuthToken(oauthTokenURL, creds.RefreshToken)
	if err != nil {
		return nil, NewExpiredOAuthToken()
	}

	// Save refreshed credentials
	_ = SaveOAuthCredentials(refreshed)

	auth.BearerToken = refreshed.AccessToken
	return auth, nil
}

// --- AnthropicClient (mirrors Rust ClawProvider) ---

// AnthropicClient implements Provider for the Anthropic Messages API.
// Mirrors Rust ClawProvider.
type AnthropicClient struct {
	baseURL    string
	auth       *AuthSource
	model      string
	httpClient *http.Client
	retry      RetryPolicy
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(baseURL string, auth *AuthSource, model string) *AnthropicClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &AnthropicClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		retry: RetryPolicy{
			MaxAttempts: defaultMaxRetries + 1,
			BaseDelay:   defaultInitialBackoff,
			MaxDelay:    defaultMaxBackoff,
		},
	}
}

// WithRetryPolicy sets a custom retry policy.
func (c *AnthropicClient) WithRetryPolicy(policy RetryPolicy) *AnthropicClient {
	c.retry = policy
	return c
}

// buildRequest constructs an HTTP request for the Messages API.
// Mirrors Rust ClawProvider::build_request.
func (c *AnthropicClient) buildRequest(ctx context.Context, req *MessageRequest) (*http.Request, error) {
	if req.MaxTokens == 0 {
		req.MaxTokens = MaxTokensForModel(req.Model)
	}
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, NewJsonError(err)
	}

	apiURL := c.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, NewIoError(err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	// Add prompt caching beta header if any cache_control is set
	if req.HasCacheControl() {
		httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	}

	// Set auth headers based on provider method
	switch c.auth.Method {
	case AuthAWSSigV4:
		if c.auth.BearerToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)
		} else if c.auth.AWSCreds != nil {
			signer := NewSigV4Signer(c.auth.AWSCreds, c.auth.AWSRegion)
			signer.Sign(httpReq)
		}
	case AuthGoogleADC:
		if c.auth.GoogleToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.auth.GoogleToken)
		}
	case AuthAzureToken:
		if c.auth.APIKey != "" {
			httpReq.Header.Set("api-key", c.auth.APIKey)
		} else if c.auth.AzureToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.auth.AzureToken)
		}
	default:
		if c.auth.APIKey != "" {
			httpReq.Header.Set("x-api-key", c.auth.APIKey)
		}
		if c.auth.BearerToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)
		}
	}

	return httpReq, nil
}

// SendMessage sends a non-streaming request with retry logic.
// Mirrors Rust ClawProvider::send_message.
func (c *AnthropicClient) SendMessage(ctx context.Context, req *MessageRequest) (*MessageResponse, error) {
	req.Stream = false
	return c.sendWithRetry(ctx, req)
}

// sendWithRetry implements the retry loop with exponential backoff.
// Mirrors Rust send_with_retry.
func (c *AnthropicClient) sendWithRetry(ctx context.Context, req *MessageRequest) (*MessageResponse, error) {
	var lastErr error

	for attempt := 0; attempt < c.retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := c.retry.Delay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		httpReq, err := c.buildRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if attempt < c.retry.MaxAttempts-1 {
				continue
			}
			return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, NewIoError(err)
		}

		if resp.StatusCode != 200 {
			lastErr = parseApiError(resp.StatusCode, respBody)
			if isRetryableStatus(resp.StatusCode) && attempt < c.retry.MaxAttempts-1 {
				// Respect Retry-After header for 429
				if resp.StatusCode == 429 {
					if ra := resp.Header.Get("Retry-After"); ra != "" {
						if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
							select {
							case <-ctx.Done():
								return nil, ctx.Err()
							case <-time.After(time.Duration(secs) * time.Second):
							}
						}
					}
				}
				continue
			}
			return nil, lastErr
		}

		var msgResp MessageResponse
		if err := json.Unmarshal(respBody, &msgResp); err != nil {
			return nil, NewJsonError(err)
		}

		// Attach request ID from header
		if rid := resp.Header.Get("request-id"); rid != "" {
			msgResp.RequestID = rid
		}

		return &msgResp, nil
	}

	return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
}

// parseApiError parses an HTTP error response into a structured ApiError.
// Mirrors Rust expect_success.
func parseApiError(statusCode int, body []byte) *ApiError {
	var errEnvelope struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errEnvelope); err == nil && errEnvelope.Error.Message != "" {
		return NewApiStatusError(statusCode, string(body), errEnvelope.Error.Type, errEnvelope.Error.Message)
	}
	return &ApiError{
		Kind:      ErrApi,
		Status:    statusCode,
		Body:      string(body),
		Retryable: isRetryableStatus(statusCode),
	}
}

// StreamMessage sends a streaming request with retry logic.
// Mirrors Rust ClawProvider::stream_message.
func (c *AnthropicClient) StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error) {
	req.Stream = true

	var lastErr error
	for attempt := 0; attempt < c.retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := c.retry.Delay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		httpReq, err := c.buildRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = parseApiError(resp.StatusCode, body)
			if isRetryableStatus(resp.StatusCode) && attempt < c.retry.MaxAttempts-1 {
				continue
			}
			return nil, lastErr
		}

		ch := ParseSSEStream(resp.Body)
		return ch, nil
	}

	return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
}

// StreamAndCollect streams a request and collects all AssistantEvents.
func (c *AnthropicClient) StreamAndCollect(ctx context.Context, req *MessageRequest) ([]AssistantEvent, Usage, error) {
	frames, err := c.StreamMessage(ctx, req)
	if err != nil {
		return nil, Usage{}, err
	}

	return CollectStreamEvents(frames)
}

// CollectStreamEvents converts SSE frames into AssistantEvents.
// Mirrors Rust ClawProvider::collect_stream_events (via StreamState in Rust).
func CollectStreamEvents(frames <-chan SSEFrame) ([]AssistantEvent, Usage, error) {
	var events []AssistantEvent
	var usage Usage
	var textBuf strings.Builder
	var toolUseID, toolUseName string
	var toolInputBuf strings.Builder

	for frame := range frames {
		if frame.Event == "error" {
			return events, usage, fmt.Errorf("%s", frame.Data)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(frame.Data), &raw); err != nil {
			continue
		}

		eventType, _ := raw["type"].(string)

		switch eventType {
		case "message_start":
			if msgRaw, ok := raw["message"].(map[string]interface{}); ok {
				if u, ok := msgRaw["usage"].(map[string]interface{}); ok {
					if v, ok := u["input_tokens"].(float64); ok {
						usage.InputTokens = int(v)
					}
					if v, ok := u["output_tokens"].(float64); ok {
						usage.OutputTokens = int(v)
					}
					if v, ok := u["cache_creation_input_tokens"].(float64); ok {
						usage.CacheCreationInputTokens = int(v)
					}
					if v, ok := u["cache_read_input_tokens"].(float64); ok {
						usage.CacheReadInputTokens = int(v)
					}
				}
			}

		case "content_block_start":
			// Flush previous text
			if textBuf.Len() > 0 {
				events = append(events, NewTextEvent(textBuf.String()))
				textBuf.Reset()
			}
			// Check if this is a tool_use block
			if block, ok := raw["content_block"].(map[string]interface{}); ok {
				if blockType, _ := block["type"].(string); blockType == "tool_use" {
					toolUseID, _ = block["id"].(string)
					toolUseName, _ = block["name"].(string)
					toolInputBuf.Reset()
				}
			}

		case "content_block_delta":
			if delta, ok := raw["delta"].(map[string]interface{}); ok {
				deltaType, _ := delta["type"].(string)
				switch deltaType {
				case "text_delta":
					text, _ := delta["text"].(string)
					textBuf.WriteString(text)
				case "input_json_delta":
					pj, _ := delta["partial_json"].(string)
					toolInputBuf.WriteString(pj)
				case "thinking_delta":
					thinking, _ := delta["thinking"].(string)
					events = append(events, NewThinkingEvent(thinking))
				case "signature_delta":
					sig, _ := delta["signature"].(string)
					events = append(events, NewSignatureEvent(sig))
				}
			}

		case "content_block_stop":
			if toolUseID != "" {
				var input map[string]interface{}
				json.Unmarshal([]byte(toolInputBuf.String()), &input)
				events = append(events, NewToolUseEvent(toolUseID, toolUseName, input))
				toolUseID = ""
				toolUseName = ""
				toolInputBuf.Reset()
			}
			if textBuf.Len() > 0 {
				events = append(events, NewTextEvent(textBuf.String()))
				textBuf.Reset()
			}

		case "message_delta":
			if delta, ok := raw["usage"].(map[string]interface{}); ok {
				if v, ok := delta["output_tokens"].(float64); ok {
				usage.OutputTokens = int(v)
			}
			if v, ok := delta["input_tokens"].(float64); ok && int(v) > usage.InputTokens {
				usage.InputTokens = int(v)
			}
			}

		case "message_stop":
			if textBuf.Len() > 0 {
				events = append(events, NewTextEvent(textBuf.String()))
			}
		}
	}

	return events, usage, nil
}

// IncrementalStreamEvent is an incremental event from a streaming response.
// Text deltas are emitted immediately, not buffered.
type IncrementalStreamEvent struct {
	Type      string // "text_delta", "tool_use", "message_start", "message_stop", "usage"
	Text      string
	ToolID    string
	ToolName  string
	ToolInput map[string]interface{}
	Usage     Usage
}

// StreamEventsIncremental reads SSE frames and sends incremental IncrementalStreamEvents.
// Unlike CollectStreamEvents which buffers all events, this emits text deltas
// immediately as they arrive, enabling true real-time streaming.
func StreamEventsIncremental(frames <-chan SSEFrame) <-chan IncrementalStreamEvent {
	ch := make(chan IncrementalStreamEvent, 64)
	go func() {
		defer close(ch)
		var toolUseID, toolUseName string
		var toolInputBuf strings.Builder

		for frame := range frames {
			if frame.Event == "error" {
				ch <- IncrementalStreamEvent{Type: "error", Text: frame.Data}
				return
			}

			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(frame.Data), &raw); err != nil {
				continue
			}

			eventType, _ := raw["type"].(string)

			switch eventType {
			case "message_start":
				if msgRaw, ok := raw["message"].(map[string]interface{}); ok {
					if u, ok := msgRaw["usage"].(map[string]interface{}); ok {
						var usage Usage
						if v, ok := u["input_tokens"].(float64); ok {
							usage.InputTokens = int(v)
						}
						if v, ok := u["output_tokens"].(float64); ok {
							usage.OutputTokens = int(v)
						}
						ch <- IncrementalStreamEvent{Type: "usage", Usage: usage}
					}
				}

			case "content_block_start":
				if block, ok := raw["content_block"].(map[string]interface{}); ok {
					if blockType, _ := block["type"].(string); blockType == "tool_use" {
						toolUseID, _ = block["id"].(string)
						toolUseName, _ = block["name"].(string)
						toolInputBuf.Reset()
					}
				}

			case "content_block_delta":
				if delta, ok := raw["delta"].(map[string]interface{}); ok {
					deltaType, _ := delta["type"].(string)
					switch deltaType {
					case "text_delta":
						text, _ := delta["text"].(string)
						// Emit text delta immediately - key for true streaming
						ch <- IncrementalStreamEvent{Type: "text_delta", Text: text}
					case "input_json_delta":
						pj, _ := delta["partial_json"].(string)
						toolInputBuf.WriteString(pj)
					case "thinking_delta":
						thinking, _ := delta["thinking"].(string)
						ch <- IncrementalStreamEvent{Type: "thinking", Text: thinking}
					}
				}

			case "content_block_stop":
				if toolUseID != "" {
					var input map[string]interface{}
					json.Unmarshal([]byte(toolInputBuf.String()), &input)
					ch <- IncrementalStreamEvent{
						Type:      "tool_use",
						ToolID:    toolUseID,
						ToolName:  toolUseName,
						ToolInput: input,
					}
					toolUseID = ""
					toolUseName = ""
					toolInputBuf.Reset()
				}

			case "message_delta":
				if delta, ok := raw["usage"].(map[string]interface{}); ok {
					var usage Usage
					if v, ok := delta["output_tokens"].(float64); ok {
						usage.OutputTokens = int(v)
					}
					if v, ok := delta["input_tokens"].(float64); ok {
						usage.InputTokens = int(v)
					}
					ch <- IncrementalStreamEvent{Type: "usage", Usage: usage}
				}

			case "message_stop":
				ch <- IncrementalStreamEvent{Type: "message_stop"}
			}
		}
	}()
	return ch
}

// isRetryableStatus returns true for HTTP status codes that can be retried.
// Mirrors Rust is_retryable — includes 408, 409, 429, 500, 502, 503, 504.
func isRetryableStatus(status int) bool {
	switch status {
	case 408, 409, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}
