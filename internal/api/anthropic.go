package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicVersion = "2023-06-01"

// AnthropicClient implements Provider for the Anthropic Messages API.
type AnthropicClient struct {
	baseURL    string
	auth       *AuthSource
	model      string
	httpClient *http.Client
	retry      RetryPolicy
}

func NewAnthropicClient(baseURL string, auth *AuthSource, model string) *AnthropicClient {
	return &AnthropicClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		retry: DefaultRetryPolicy(),
	}
}

func (c *AnthropicClient) buildRequest(ctx context.Context, req *MessageRequest) (*http.Request, error) {
	if req.MaxTokens == 0 {
		req.MaxTokens = MaxTokensForModel(req.Model)
	}
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &ApiError{Code: "json_error", Message: err.Error()}
	}

	url := c.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, &ApiError{Code: "request_error", Message: err.Error()}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	if c.auth.APIKey != "" {
		httpReq.Header.Set("x-api-key", c.auth.APIKey)
	}
	if c.auth.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.BearerToken)
	}

	return httpReq, nil
}

func (c *AnthropicClient) SendMessage(ctx context.Context, req *MessageRequest) (*MessageResponse, error) {
	req.Stream = false

	var lastErr error
	for attempt := 0; attempt < c.retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(c.retry.Delay(attempt - 1))
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

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, &ApiError{Code: "read_error", Message: err.Error()}
		}

		if resp.StatusCode != 200 {
			lastErr = &ApiError{Code: "http_error", Message: string(respBody), Status: resp.StatusCode}
			if isRetryableStatus(resp.StatusCode) {
				continue
			}
			return nil, lastErr.(*ApiError)
		}

		var msgResp MessageResponse
		if err := json.Unmarshal(respBody, &msgResp); err != nil {
			return nil, &ApiError{Code: "json_error", Message: err.Error()}
		}
		return &msgResp, nil
	}

	return nil, &ApiError{Code: "retries_exhausted", Message: fmt.Sprintf("failed after %d attempts: %v", c.retry.MaxAttempts, lastErr)}
}

func (c *AnthropicClient) StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error) {
	req.Stream = true

	httpReq, err := c.buildRequest(ctx, req)
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
		return nil, &ApiError{Code: "http_error", Message: string(body), Status: resp.StatusCode}
	}

	ch := ParseSSEStream(resp.Body)
	return ch, nil
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
					usage.InputTokens = int(u["input_tokens"].(float64))
					usage.OutputTokens = int(u["output_tokens"].(float64))
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
				usage.OutputTokens = int(delta["output_tokens"].(float64))
			}

		case "message_stop":
			if textBuf.Len() > 0 {
				events = append(events, NewTextEvent(textBuf.String()))
			}
		}
	}

	return events, usage, nil
}

func isRetryableStatus(status int) bool {
	return status == 429 || status == 500 || status == 502 || status == 503
}
