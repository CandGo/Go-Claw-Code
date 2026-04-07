package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	xAIDefaultBaseURL    = "https://api.x.ai/v1"
	openidDefaultBaseURL = "https://api.openai.com/v1"
)

// OpenAICompatClient implements Provider for OpenAI-compatible APIs.
// Mirrors Rust OpenAiCompatClient.
type OpenAICompatClient struct {
	baseURL    string
	auth       *AuthSource
	model      string
	httpClient *http.Client
	retry      RetryPolicy
}

// NewOpenAICompatClient creates a new OpenAI-compatible client.
func NewOpenAICompatClient(baseURL string, auth *AuthSource, model string) *OpenAICompatClient {
	if baseURL == "" {
		baseURL = openidDefaultBaseURL
	}
	return &OpenAICompatClient{
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

// NewXAICompatClient creates a client configured for xAI.
// Mirrors Rust OpenAiCompatConfig::xai().
func NewXAICompatClient(auth *AuthSource, model string) *OpenAICompatClient {
	apiKey := auth.APIKey
	if apiKey == "" {
		apiKey = auth.BearerToken
	}
	return NewOpenAICompatClient(xAIDefaultBaseURL, &AuthSource{APIKey: apiKey}, model)
}

// WithRetryPolicy sets a custom retry policy.
func (c *OpenAICompatClient) WithRetryPolicy(policy RetryPolicy) *OpenAICompatClient {
	c.retry = policy
	return c
}

// --- OpenAI wire types (mirrors Rust serde types) ---

// oaiChatMessage is the OpenAI chat completion message format.
type oaiChatMessage struct {
	Role       string        `json:"role"`
	Content    interface{}   `json:"content"`                // string or []oaiContentPart or nil
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // for role=tool messages
}

type oaiContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL interface{} `json:"image_url,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiChatRequest struct {
	Model      string           `json:"model"`
	Messages   []oaiChatMessage `json:"messages"`
	MaxTokens  int              `json:"max_tokens,omitempty"`
	Stream     bool             `json:"stream"`
	Tools      []oaiToolDef     `json:"tools,omitempty"`
	ToolChoice interface{}      `json:"tool_choice,omitempty"`
}

type oaiToolDef struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type oaiChatResponse struct {
	ID      string      `json:"id"`
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiChoice struct {
	Index        int            `json:"index"`
	Message      oaiChatMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Request translation (mirrors Rust build_chat_completion_request) ---

// translateToOpenAI converts an Anthropic-format request to OpenAI format.
// Mirrors Rust translate_message + build_chat_completion_request.
func translateToOpenAI(req *MessageRequest) *oaiChatRequest {
	oai := &oaiChatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	// System prompt -> system message
	if req.System != nil {
		oai.Messages = append(oai.Messages, oaiChatMessage{
			Role:    "system",
			Content: string(req.System),
		})
	}

	// Convert messages
	for _, msg := range req.Messages {
		oaiMsg := oaiChatMessage{Role: string(msg.Role)}

		switch msg.Role {
		case RoleUser:
			parts := make([]oaiContentPart, 0)
			for _, block := range msg.Content {
				switch block.Type {
				case "text":
					parts = append(parts, oaiContentPart{Type: "text", Text: block.Text})
				case "tool_result":
					// OpenAI uses role=tool with tool_call_id
					contentStr := flattenToolResultContent(block)
					oai.Messages = append(oai.Messages, oaiChatMessage{
						Role:       "tool",
						Content:    contentStr,
						ToolCallID: block.ToolUseID,
					})
					continue
				default:
					raw, _ := json.Marshal(block)
					parts = append(parts, oaiContentPart{Type: "text", Text: string(raw)})
				}
			}
			if len(parts) == 1 && parts[0].Type == "text" {
				oaiMsg.Content = parts[0].Text
			} else if len(parts) > 0 {
				oaiMsg.Content = parts
			}
			if oaiMsg.Content != nil {
				oai.Messages = append(oai.Messages, oaiMsg)
			}

		case RoleAssistant:
			var textParts []string
			var toolCalls []oaiToolCall
			for _, block := range msg.Content {
				switch block.Type {
				case "text":
					textParts = append(textParts, block.Text)
				case "tool_use":
					args, _ := json.Marshal(block.Input)
					toolCalls = append(toolCalls, oaiToolCall{
						ID:   block.ID,
						Type: "function",
						Function: oaiFunctionCall{
							Name:      block.Name,
							Arguments: string(args),
						},
					})
				}
			}
			oaiMsg.Content = strings.Join(textParts, "")
			if len(toolCalls) > 0 {
				oaiMsg.ToolCalls = toolCalls
			}
			oai.Messages = append(oai.Messages, oaiMsg)

		default:
			raw, _ := json.Marshal(msg.Content)
			oaiMsg.Content = string(raw)
			oai.Messages = append(oai.Messages, oaiMsg)
		}
	}

	// Convert tools
	for _, t := range req.Tools {
		oai.Tools = append(oai.Tools, oaiToolDef{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// Tool choice (mirrors Rust openai_tool_choice)
	if req.ToolChoice.Type == "auto" {
		oai.ToolChoice = "auto"
	} else if req.ToolChoice.Type == "any" {
		oai.ToolChoice = "required"
	} else if req.ToolChoice.Type == "tool" {
		oai.ToolChoice = map[string]interface{}{
			"type":     "function",
			"function": map[string]string{"name": req.ToolChoice.ToolName},
		}
	}

	return oai
}

// flattenToolResultContent extracts text from a tool_result content block.
func flattenToolResultContent(block InputContentBlock) string {
	if len(block.Content) == 0 {
		return block.Text
	}
	var parts []string
	for _, c := range block.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		} else if c.Value != nil {
			raw, _ := json.Marshal(c.Value)
			parts = append(parts, string(raw))
		}
	}
	result := strings.Join(parts, "\n")
	if result == "" {
		raw, _ := json.Marshal(block.Content)
		return string(raw)
	}
	return result
}

// buildHTTPRequest constructs an HTTP request for the OpenAI chat completions API.
func (c *OpenAICompatClient) buildHTTPRequest(ctx context.Context, oai *oaiChatRequest) (*http.Request, error) {
	body, err := json.Marshal(oai)
	if err != nil {
		return nil, NewJsonError(err)
	}

	endpoint := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, NewIoError(err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.auth.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.APIKey)
	} else if c.auth.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)
	}

	return httpReq, nil
}

// SendMessage sends a non-streaming request via OpenAI-compatible API with retry.
func (c *OpenAICompatClient) SendMessage(ctx context.Context, req *MessageRequest) (*MessageResponse, error) {
	req.Stream = false
	req.Model = c.model
	if req.MaxTokens == 0 {
		req.MaxTokens = MaxTokensForModel(req.Model)
	}

	oai := translateToOpenAI(req)

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

		httpReq, err := c.buildHTTPRequest(ctx, oai)
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

		var oaiResp oaiChatResponse
		if err := json.Unmarshal(respBody, &oaiResp); err != nil {
			return nil, NewJsonError(err)
		}

		return translateResponse(&oaiResp), nil
	}

	return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
}

// StreamMessage sends a streaming request via OpenAI-compatible API with retry.
func (c *OpenAICompatClient) StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error) {
	req.Stream = true
	req.Model = c.model
	if req.MaxTokens == 0 {
		req.MaxTokens = MaxTokensForModel(req.Model)
	}

	oai := translateToOpenAI(req)

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

		httpReq, err := c.buildHTTPRequest(ctx, oai)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		// Handle non-200 status codes (429 rate limit, 500, 401, etc.)
		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = parseApiError(resp.StatusCode, respBody)
			if isRetryableStatus(resp.StatusCode) && attempt < c.retry.MaxAttempts-1 {
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

		// Handle HTTP 200 with non-SSE body (some providers return JSON errors this way)
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/event-stream") {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500]
			}
			if strings.Contains(bodyStr, "error") || (strings.Contains(bodyStr, "\"code\"") && strings.Contains(bodyStr, "\"msg\"")) {
				return nil, fmt.Errorf("API error: %s", bodyStr)
			}
			return nil, fmt.Errorf("unexpected Content-Type %s for streaming response", contentType)
		}

		ch := translateOpenAIStream(resp.Body)
		return ch, nil
	}

	return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
}

// --- Response normalization ---

// translateResponse converts an OpenAI response to Anthropic format.
func translateResponse(oai *oaiChatResponse) *MessageResponse {
	resp := &MessageResponse{
		ID:    oai.ID,
		Model: "",
		Role:  "assistant",
		Type:  "message",
		Usage: Usage{
			InputTokens:  oai.Usage.PromptTokens,
			OutputTokens: oai.Usage.CompletionTokens,
		},
	}

	for _, choice := range oai.Choices {
		if msg, ok := choice.Message.Content.(string); ok && msg != "" {
			resp.Content = append(resp.Content, OutputContentBlock{Type: "text", Text: msg})
		}

		for _, tc := range choice.Message.ToolCalls {
			input := parseToolArguments(tc.Function.Arguments)
			resp.Content = append(resp.Content, OutputContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		if choice.FinishReason != "" {
			switch choice.FinishReason {
			case "tool_calls":
				resp.StopReason = "tool_use"
			case "stop":
				resp.StopReason = "end_turn"
			default:
				resp.StopReason = choice.FinishReason
			}
		}
	}

	return resp
}

// parseToolArguments parses tool call arguments with fallback.
func parseToolArguments(args string) map[string]interface{} {
	if args == "" {
		return map[string]interface{}{}
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return map[string]interface{}{"raw": args}
	}
	return input
}

// endpointBuilder builds the API endpoint URL.
func endpointBuilder(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	if !strings.Contains(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

// DetectOpenAIBaseURL returns the base URL from environment or default.
func DetectOpenAIBaseURL(provider ProviderKind) string {
	switch provider {
	case ProviderXAI:
		if u := envOr("XAI_BASE_URL", ""); u != "" {
			return u
		}
		return xAIDefaultBaseURL
	case ProviderOpenAI:
		if u := envOr("OPENAI_BASE_URL", ""); u != "" {
			return u
		}
		return openidDefaultBaseURL
	default:
		if u := envOr("OPENAI_BASE_URL", ""); u != "" {
			return u
		}
		return openidDefaultBaseURL
	}
}

// DetectOpenAICredentials resolves credentials for OpenAI-compatible APIs.
func DetectOpenAICredentials(provider ProviderKind) *AuthSource {
	switch provider {
	case ProviderXAI:
		if key := envOr("XAI_API_KEY", ""); key != "" {
			return &AuthSource{APIKey: key}
		}
	case ProviderOpenAI:
		if key := envOr("OPENAI_API_KEY", ""); key != "" {
			return &AuthSource{APIKey: key}
		}
	default:
		if key := envOr("OPENAI_API_KEY", ""); key != "" {
			return &AuthSource{APIKey: key}
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
