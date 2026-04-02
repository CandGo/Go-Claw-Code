package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatClient implements Provider for OpenAI-compatible APIs.
type OpenAICompatClient struct {
	baseURL    string
	auth       *AuthSource
	model      string
	httpClient *http.Client
	retry      RetryPolicy
}

func NewOpenAICompatClient(baseURL string, auth *AuthSource, model string) *OpenAICompatClient {
	return &OpenAICompatClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		retry: DefaultRetryPolicy(),
	}
}

// oaiChatMessage is the OpenAI chat completion message format.
type oaiChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []oaiContentPart
}

type oaiContentPart struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	ImageURL interface{}            `json:"image_url,omitempty"`
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
	Model       string           `json:"model"`
	Messages    []oaiChatMessage `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream"`
	Tools       []oaiToolDef     `json:"tools,omitempty"`
	ToolChoice  interface{}      `json:"tool_choice,omitempty"`
}

type oaiToolDef struct {
	Type     string       `json:"type"`
	Function oaiFunction  `json:"function"`
}

type oaiFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type oaiChatResponse struct {
	ID      string         `json:"id"`
	Choices []oaiChoice    `json:"choices"`
	Usage   oaiUsage       `json:"usage"`
}

type oaiChoice struct {
	Message      oaiChatMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// translateToOpenAI converts an Anthropic-format request to OpenAI format.
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
					// For tool_result, marshal the content as text
					var contentStr string
					if len(block.Content) > 0 {
						raw, _ := json.Marshal(block.Content)
						contentStr = string(raw)
					}
					parts = append(parts, oaiContentPart{Type: "text", Text: contentStr})
				default:
					raw, _ := json.Marshal(block)
					parts = append(parts, oaiContentPart{Type: "text", Text: string(raw)})
				}
			}
			if len(parts) == 1 && parts[0].Type == "text" {
				oaiMsg.Content = parts[0].Text
			} else {
				oaiMsg.Content = parts
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
			if len(toolCalls) > 0 {
				// Use raw marshaling to get both content and tool_calls
				msgBytes, _ := json.Marshal(map[string]interface{}{
					"role":       "assistant",
					"content":    strings.Join(textParts, ""),
					"tool_calls": toolCalls,
				})
				json.Unmarshal(msgBytes, &oaiMsg)
			} else {
				oaiMsg.Content = strings.Join(textParts, "")
			}

		default:
			raw, _ := json.Marshal(msg.Content)
			oaiMsg.Content = string(raw)
		}

		oai.Messages = append(oai.Messages, oaiMsg)
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

	// Tool choice
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

func (c *OpenAICompatClient) buildRequest(ctx context.Context, oai *oaiChatRequest) (*http.Request, error) {
	body, err := json.Marshal(oai)
	if err != nil {
		return nil, JsonError(err)
	}

	url := c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, JsonError(err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.auth.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.APIKey)
	}
	if c.auth.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)
	}

	return httpReq, nil
}

// SendMessage sends a non-streaming request via OpenAI-compatible API.
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
			time.Sleep(c.retry.Delay(attempt - 1))
		}

		httpReq, err := c.buildRequest(ctx, oai)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if isRetryable(err) {
				continue
			}
			return nil, &ApiError{Code: "connection_error", Message: err.Error()}
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			lastErr = HttpError(resp.StatusCode, string(respBody))
			if isRetryable(lastErr) {
				continue
			}
			return nil, lastErr
		}

		var oaiResp oaiChatResponse
		if err := json.Unmarshal(respBody, &oaiResp); err != nil {
			return nil, JsonError(err)
		}

		return translateResponse(&oaiResp), nil
	}

	return nil, RetriesExhausted(c.retry.MaxAttempts, lastErr)
}

// StreamMessage sends a streaming request via OpenAI-compatible API.
func (c *OpenAICompatClient) StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error) {
	req.Stream = true
	req.Model = c.model
	if req.MaxTokens == 0 {
		req.MaxTokens = MaxTokensForModel(req.Model)
	}

	oai := translateToOpenAI(req)

	httpReq, err := c.buildRequest(ctx, oai)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &ApiError{Code: "connection_error", Message: err.Error()}
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, HttpError(resp.StatusCode, string(body))
	}

	ch := ParseSSEStream(resp.Body)
	return ch, nil
}

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
	}

	return resp
}
