package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Protocol types for JSON-RPC over stdio with Content-Length framing.

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MCP Protocol types

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Resource represents an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt represents an MCP prompt template.
type Prompt struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PromptMessage is a message in a prompt template.
type PromptMessage struct {
	Role    string `json:"role"`
	Content struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
}

// ServerInfo holds server metadata from initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Client is an MCP client that communicates over stdio JSON-RPC
// using Content-Length header framing per MCP specification.
type Client struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	idCounter  atomic.Int64
	pending    map[int64]chan jsonRPCResponse
	mu         sync.Mutex
	tools      []Tool
	resources  []Resource
	prompts    []Prompt
	serverInfo ServerInfo
	writeMu    sync.Mutex // serialize writes to stdin
	timeout    time.Duration
}

// NewClient creates a new MCP client by spawning the server command.
func NewClient(ctx context.Context, command string, args []string, env map[string]string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Redirect stderr for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	client := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int64]chan jsonRPCResponse),
		timeout: 30 * time.Second,
	}

	go client.readLoop()

	return client, nil
}

// Initialize sends the initialize handshake to the MCP server.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"clientInfo": map[string]interface{}{
			"name":    "claw-code-go",
			"version": "0.5.0",
		},
	}

	resp, err := c.call("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var initResult struct {
		ProtocolVersion string      `json:"protocolVersion"`
		Capabilities    interface{} `json:"capabilities"`
		ServerInfo      ServerInfo  `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("failed to parse initialize response: %w", err)
	}

	c.serverInfo = initResult.ServerInfo

	c.notify("notifications/initialized", nil)

	return nil
}

// ListTools requests the list of tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.call("tools/list", nil)
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
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	resp, err := c.call("tools/call", params)
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
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	resp, err := c.call("resources/list", nil)
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

// ReadResource reads a specific resource from the MCP server.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	params := map[string]interface{}{
		"uri": uri,
	}

	resp, err := c.call("resources/read", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType,omitempty"`
			Text     string `json:"text,omitempty"`
			Blob     string `json:"blob,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse resource: %w", err)
	}

	var texts []string
	for _, c := range result.Contents {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// ListPrompts requests the list of available prompts.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	resp, err := c.call("prompts/list", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	c.prompts = result.Prompts
	return result.Prompts, nil
}

// GetPrompt retrieves a specific prompt template.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) ([]PromptMessage, error) {
	params := map[string]interface{}{
		"name":     name,
		"arguments": args,
	}

	resp, err := c.call("prompts/get", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Description string          `json:"description,omitempty"`
		Messages    []PromptMessage `json:"messages"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse prompt: %w", err)
	}

	return result.Messages, nil
}

// Tools returns the cached tool list.
func (c *Client) Tools() []Tool {
	return c.tools
}

// Resources returns the cached resource list.
func (c *Client) Resources() []Resource {
	return c.resources
}

// Prompts returns the cached prompt list.
func (c *Client) Prompts() []Prompt {
	return c.prompts
}

// ServerInfo returns the server metadata.
func (c *Client) ServerInfo() ServerInfo {
	return c.serverInfo
}

// Close shuts down the MCP server.
func (c *Client) Close() error {
	c.notify("shutdown", nil)
	c.stdin.Close()
	return c.cmd.Wait()
}

// SetTimeout sets the request timeout for RPC calls.
func (c *Client) SetTimeout(d time.Duration) {
	c.timeout = d
}

// writeMessage sends a JSON-RPC message with Content-Length framing.
// Format: Content-Length: <N>\r\n\r\n<json>
func (c *Client) writeMessage(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write body: %w", err)
	}
	return nil
}

// readMessage reads a JSON-RPC message with Content-Length framing.
func (c *Client) readMessage() ([]byte, error) {
	// Read headers until empty line
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")

		// Empty line marks end of headers
		if line == "" {
			break
		}

		// Parse Content-Length header
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", val)
			}
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	// Read the body
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, buf); err != nil {
		return nil, fmt.Errorf("failed to read message body: %w", err)
	}

	return buf, nil
}

func (c *Client) call(method string, params interface{}) (*jsonRPCResponse, error) {
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

	if err := c.writeMessage(data); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return &resp, nil
	case <-time.After(c.timeout):
		return nil, fmt.Errorf("request timeout after %s for method %s", c.timeout, method)
	}
}

func (c *Client) notify(method string, params interface{}) {
	req := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, _ := json.Marshal(req)
	c.writeMessage(data)
}

func (c *Client) readLoop() {
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}

		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- resp
			}
			c.mu.Unlock()
		}
		// Notifications (no ID) are silently discarded for now
	}
}
