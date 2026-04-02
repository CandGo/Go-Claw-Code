package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// Protocol types for JSON-RPC over stdio.

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

// ServerInfo holds server metadata from initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Client is an MCP client that communicates over stdio JSON-RPC.
type Client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	idCounter atomic.Int64
	pending   map[int64]chan jsonRPCResponse
	mu        sync.Mutex
	tools     []Tool
	resources []Resource
	serverInfo ServerInfo
}

// NewClient creates a new MCP client by spawning the server command.
func NewClient(ctx context.Context, command string, args []string, env map[string]string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	// Set environment
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

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	client := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		pending: make(map[int64]chan jsonRPCResponse),
	}

	// Start reading responses
	go client.readLoop()

	return client, nil
}

// Initialize sends the initialize handshake to the MCP server.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "claw-code-go",
			"version": "0.3.0",
		},
	}

	resp, err := c.call("initialize", params)
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

	// Send initialized notification
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

// Tools returns the cached tool list.
func (c *Client) Tools() []Tool {
	return c.tools
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

	if _, err := fmt.Fprintf(c.stdin, "%s\n", data); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

func (c *Client) notify(method string, params interface{}) {
	req := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, _ := json.Marshal(req)
	fmt.Fprintf(c.stdin, "%s\n", data)
}

func (c *Client) readLoop() {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- resp
			}
			c.mu.Unlock()
		}
	}
}
