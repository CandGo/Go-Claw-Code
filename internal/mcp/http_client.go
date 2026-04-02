package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HTTPClient is an MCP client that communicates via HTTP+SSE transport.
// It sends JSON-RPC requests via HTTP POST and receives responses via SSE.
type HTTPClient struct {
	baseURL    string
	headers    map[string]string
	httpClient *http.Client
	idCounter  atomic.Int64
	pending    map[int64]chan jsonRPCResponse
	mu         sync.Mutex
	tools      []Tool
	resources  []Resource
	serverInfo ServerInfo
	eventCh    chan jsonRPCResponse
	cancelSSE  context.CancelFunc
}

// NewHTTPClient creates an MCP client connected via HTTP+SSE.
func NewHTTPClient(baseURL string, headers map[string]string) (*HTTPClient, error) {
	client := &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		pending: make(map[int64]chan jsonRPCResponse),
		eventCh: make(chan jsonRPCResponse, 64),
	}

	return client, nil
}

// Initialize sends the initialize handshake via HTTP POST and starts SSE listener.
func (c *HTTPClient) Initialize(ctx context.Context) error {
	// Start SSE event stream in background
	sseCtx, cancel := context.WithCancel(ctx)
	c.cancelSSE = cancel

	// First, start the SSE endpoint connection
	sseURL := c.baseURL + "/sse"
	if err := c.startSSEListener(sseCtx, sseURL); err != nil {
		cancel()
		return fmt.Errorf("failed to start SSE listener: %w", err)
	}

	// Send initialize via HTTP POST
	params := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "claw-code-go",
			"version": "0.4.0",
		},
	}

	resp, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var initResult struct {
		ProtocolVersion string     `json:"protocolVersion"`
		Capabilities    interface{} `json:"capabilities"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("failed to parse initialize response: %w", err)
	}

	c.serverInfo = initResult.ServerInfo
	return nil
}

// startSSEListener connects to the SSE endpoint and reads events.
func (c *HTTPClient) startSSEListener(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect failed: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("SSE endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var rpcResp jsonRPCResponse
			if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
				continue
			}

			if rpcResp.ID > 0 {
				c.mu.Lock()
				if ch, ok := c.pending[rpcResp.ID]; ok {
					ch <- rpcResp
				}
				c.mu.Unlock()
			}
		}
	}()

	return nil
}

// ListTools requests the list of tools from the MCP server.
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	c.tools = result.Tools
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	resp, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse tool result: %w", err)
	}

	var texts []string
	for _, c := range result.Content {
		texts = append(texts, c.Text)
	}
	output := strings.Join(texts, "\n")

	if result.IsError {
		return output, fmt.Errorf("tool error: %s", output)
	}
	return output, nil
}

// ListResources requests the list of resources.
func (c *HTTPClient) ListResources(ctx context.Context) ([]Resource, error) {
	resp, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	c.resources = result.Resources
	return result.Resources, nil
}

// Tools returns the cached tool list.
func (c *HTTPClient) Tools() []Tool {
	return c.tools
}

// ServerInfo returns the server metadata.
func (c *HTTPClient) ServerInfo() ServerInfo {
	return c.serverInfo
}

// Close shuts down the HTTP MCP client.
func (c *HTTPClient) Close() error {
	if c.cancelSSE != nil {
		c.cancelSSE()
	}
	return nil
}

func (c *HTTPClient) call(ctx context.Context, method string, params interface{}) (*jsonRPCResponse, error) {
	id := c.idCounter.Add(1)
	ch := make(chan jsonRPCResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Send via HTTP POST to the messages endpoint
	postURL := c.baseURL + "/message"
	if method == "initialize" {
		postURL = c.baseURL + "/message"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", postURL, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// For HTTP+SSE transport, the response comes back via the SSE stream.
	// If the HTTP response itself contains the result (some servers), use it.
	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			var rpcResp jsonRPCResponse
			if err := json.Unmarshal(body, &rpcResp); err == nil && rpcResp.ID == id {
				if rpcResp.Error != nil {
					return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
				}
				return &rpcResp, nil
			}
		}
	}

	// Otherwise, wait for the response via SSE
	select {
	case rpcResp := <-ch:
		if rpcResp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}
		return &rpcResp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to %s", method)
	}
}

// SendNotification sends a JSON-RPC notification (no ID, no response expected).
func (c *HTTPClient) SendNotification(ctx context.Context, method string, params interface{}) error {
	req := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	postURL := c.baseURL + "/message"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", postURL, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
