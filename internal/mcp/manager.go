package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ToolRoute maps a qualified tool name to its source server and raw tool name.
type ToolRoute struct {
	ServerName string
	RawName    string
}

// ManagedMcpServer holds the state of a single MCP server connection.
type ManagedMcpServer struct {
	Config      MCPServerConfig
	Client      *Client
	HTTPClient  *HTTPClient
	Initialized bool
}

// MCPServerConfig describes how to connect to an MCP server.
type MCPServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
	Type    string // "stdio" or "http"
	URL     string
	Headers map[string]string
}

// UnsupportedServer records a server whose transport type is not supported.
type UnsupportedServer struct {
	Name string
	Type string
}

// ServerManagerError represents an error from the server manager.
type ServerManagerError struct {
	ServerName string
	Method     string
	Err        error
}

func (e *ServerManagerError) Error() string {
	return fmt.Sprintf("MCP server %s: %s: %v", e.ServerName, e.Method, e.Err)
}

func (e *ServerManagerError) Unwrap() error {
	return e.Err
}

// ServerManager manages multiple MCP server connections with lazy spawning
// and tool routing. It implements the same pattern as the Rust McpServerManager:
// servers are spawned lazily on first access, tools are namespaced as mcp__<server>__<tool>,
// and routing dispatches qualified tool calls to the correct server.
type ServerManager struct {
	mu                 sync.Mutex
	servers            map[string]*ManagedMcpServer
	unsupportedServers []UnsupportedServer
	toolIndex          map[string]ToolRoute // qualified name -> route
}

// NewServerManager creates a manager from a map of server configs.
func NewServerManager(configs map[string]MCPServerConfig) *ServerManager {
	sm := &ServerManager{
		servers:   make(map[string]*ManagedMcpServer),
		toolIndex: make(map[string]ToolRoute),
	}
	for name, cfg := range configs {
		switch cfg.Type {
		case "stdio", "":
			sm.servers[name] = &ManagedMcpServer{
				Config:      cfg,
				Initialized: false,
			}
		case "sse", "http":
			sm.servers[name] = &ManagedMcpServer{
				Config:      cfg,
				Initialized: false,
			}
		default:
			sm.unsupportedServers = append(sm.unsupportedServers, UnsupportedServer{
				Name: name,
				Type: cfg.Type,
			})
		}
	}
	return sm
}

// UnsupportedServers returns servers whose transport type is not stdio.
func (sm *ServerManager) UnsupportedServers() []UnsupportedServer {
	return sm.unsupportedServers
}

// ManagedTool represents a discovered tool with its source server.
type ManagedTool struct {
	Name        string                 // qualified name (mcp__server__tool)
	RawName     string                 // original tool name on the server
	ServerName  string                 // which server provides this tool
	Description string                 // tool description
	InputSchema map[string]interface{} // tool input schema
}

// DiscoverTools discovers tools from all managed servers with cursor-based pagination.
// For each server, it lazily spawns the process if not yet started, performs the
// initialize handshake, then paginates through tools/list building the tool index.
func (sm *ServerManager) DiscoverTools(ctx context.Context) ([]ManagedTool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var allTools []ManagedTool

	for name, srv := range sm.servers {
		// Ensure server is started and initialized
		if err := sm.ensureServerReady(ctx, name, srv); err != nil {
			return allTools, &ServerManagerError{
				ServerName: name,
				Method:     "discover",
				Err:        err,
			}
		}

		// Clear old routes for this server
		sm.clearRoutesForServer(name)

		// HTTP/SSE transport: list tools directly (no cursor pagination)
		if srv.HTTPClient != nil {
			tools, err := srv.HTTPClient.ListTools(ctx)
			if err != nil {
				return allTools, &ServerManagerError{ServerName: name, Method: "tools/list", Err: err}
			}
			for _, t := range tools {
				qualifiedName := MCPQualifiedName(name, t.Name)
				sm.toolIndex[qualifiedName] = ToolRoute{ServerName: name, RawName: t.Name}
				allTools = append(allTools, ManagedTool{
					Name: qualifiedName, RawName: t.Name, ServerName: name,
					Description: t.Description, InputSchema: t.InputSchema,
				})
			}
			continue
		}

		// Stdio transport: List tools with cursor-based pagination
		cursor := ""
		for {
			params := map[string]interface{}{}
			if cursor != "" {
				params["cursor"] = cursor
			}

			tools, nextCursor, err := listToolsWithCursor(ctx, srv.Client, params)
			if err != nil {
				return allTools, &ServerManagerError{
					ServerName: name,
					Method:     "tools/list",
					Err:        err,
				}
			}

			// Register each tool in the index
			for _, t := range tools {
				qualifiedName := MCPQualifiedName(name, t.Name)
				sm.toolIndex[qualifiedName] = ToolRoute{
					ServerName: name,
					RawName:    t.Name,
				}
				allTools = append(allTools, ManagedTool{
					Name:        qualifiedName,
					RawName:     t.Name,
					ServerName:  name,
					Description: t.Description,
					InputSchema: t.InputSchema,
				})
			}

			if nextCursor == "" {
				break
			}
			cursor = nextCursor
		}
	}

	return allTools, nil
}

// listToolsWithCursor calls tools/list and returns tools plus the next cursor.
func listToolsWithCursor(ctx context.Context, client *Client, params map[string]interface{}) ([]Tool, string, error) {
	resp, err := client.call("tools/list", params)
	if err != nil {
		return nil, "", err
	}

	var result struct {
		Tools      []Tool `json:"tools"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	client.tools = result.Tools
	return result.Tools, result.NextCursor, nil
}

// CallTool routes a tool call to the correct server using the qualified name.
// It looks up the tool in the index, ensures the target server is ready,
// then dispatches tools/call with the raw (un-prefixed) tool name.
func (sm *ServerManager) CallTool(ctx context.Context, qualifiedName string, arguments map[string]interface{}) (string, error) {
	sm.mu.Lock()
	route, ok := sm.toolIndex[qualifiedName]
	if !ok {
		sm.mu.Unlock()
		return "", fmt.Errorf("unknown MCP tool: %s", qualifiedName)
	}
	srv, ok := sm.servers[route.ServerName]
	if !ok {
		sm.mu.Unlock()
		return "", fmt.Errorf("unknown MCP server: %s", route.ServerName)
	}
	sm.mu.Unlock()

	if err := sm.ensureServerReady(ctx, route.ServerName, srv); err != nil {
		return "", &ServerManagerError{
			ServerName: route.ServerName,
			Method:     "tools/call",
			Err:        err,
		}
	}

	if srv.HTTPClient != nil {
		output, err := srv.HTTPClient.CallTool(ctx, route.RawName, arguments)
		if err != nil {
			return "", &ServerManagerError{ServerName: route.ServerName, Method: "tools/call", Err: err}
		}
		return output, nil
	}

	return srv.Client.CallTool(ctx, route.RawName, arguments)
}

// Shutdown kills all managed server processes and resets their state.
func (sm *ServerManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, srv := range sm.servers {
		if srv.Client != nil {
			srv.Client.Close()
			srv.Client = nil
			srv.Initialized = false
		}
		if srv.HTTPClient != nil {
			srv.HTTPClient.Close()
			srv.HTTPClient = nil
			srv.Initialized = false
		}
	}
}

// ListServerResources lists resources from a specific server.
func (sm *ServerManager) ListServerResources(ctx context.Context, serverName string) ([]Resource, error) {
	sm.mu.Lock()
	srv, ok := sm.servers[serverName]
	if !ok {
		sm.mu.Unlock()
		return nil, fmt.Errorf("unknown MCP server: %s", serverName)
	}
	sm.mu.Unlock()

	if err := sm.ensureServerReady(ctx, serverName, srv); err != nil {
		return nil, &ServerManagerError{ServerName: serverName, Method: "resources/list", Err: err}
	}

	if srv.HTTPClient != nil {
		return srv.HTTPClient.ListResources(ctx)
	}
	return srv.Client.ListResources(ctx)
}

// ReadServerResource reads a resource from a specific server.
func (sm *ServerManager) ReadServerResource(ctx context.Context, serverName, uri string) (string, error) {
	sm.mu.Lock()
	srv, ok := sm.servers[serverName]
	if !ok {
		sm.mu.Unlock()
		return "", fmt.Errorf("unknown MCP server: %s", serverName)
	}
	sm.mu.Unlock()

	if err := sm.ensureServerReady(ctx, serverName, srv); err != nil {
		return "", &ServerManagerError{ServerName: serverName, Method: "resources/read", Err: err}
	}

	if srv.HTTPClient != nil {
		return srv.HTTPClient.ReadResource(ctx, uri)
	}
	return srv.Client.ReadResource(ctx, uri)
}

// ListServerPrompts lists prompts from a specific server.
func (sm *ServerManager) ListServerPrompts(ctx context.Context, serverName string) ([]Prompt, error) {
	sm.mu.Lock()
	srv, ok := sm.servers[serverName]
	if !ok {
		sm.mu.Unlock()
		return nil, fmt.Errorf("unknown MCP server: %s", serverName)
	}
	sm.mu.Unlock()

	if err := sm.ensureServerReady(ctx, serverName, srv); err != nil {
		return nil, &ServerManagerError{ServerName: serverName, Method: "prompts/list", Err: err}
	}

	if srv.HTTPClient != nil {
		return nil, fmt.Errorf("prompt listing not supported for HTTP MCP servers yet")
	}
	return srv.Client.ListPrompts(ctx)
}

// GetServerPrompt retrieves a prompt template from a specific server.
func (sm *ServerManager) GetServerPrompt(ctx context.Context, serverName, promptName string, args map[string]string) ([]PromptMessage, error) {
	sm.mu.Lock()
	srv, ok := sm.servers[serverName]
	if !ok {
		sm.mu.Unlock()
		return nil, fmt.Errorf("unknown MCP server: %s", serverName)
	}
	sm.mu.Unlock()

	if err := sm.ensureServerReady(ctx, serverName, srv); err != nil {
		return nil, &ServerManagerError{ServerName: serverName, Method: "prompts/get", Err: err}
	}

	if srv.HTTPClient != nil {
		return nil, fmt.Errorf("prompt retrieval not supported for HTTP MCP servers yet")
	}
	return srv.Client.GetPrompt(ctx, promptName, args)
}

// ServerNames returns the names of all managed servers.
func (sm *ServerManager) ServerNames() []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	names := make([]string, 0, len(sm.servers))
	for name := range sm.servers {
		names = append(names, name)
	}
	return names
}

// ToolCount returns the total number of discovered tools.
func (sm *ServerManager) ToolCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.toolIndex)
}

// RouteFor returns the tool route for a qualified name, or false if not found.
func (sm *ServerManager) RouteFor(qualifiedName string) (ToolRoute, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	r, ok := sm.toolIndex[qualifiedName]
	return r, ok
}

// ensureServerReady lazily spawns and initializes a server if needed.
// Phase 1: spawn the process if no client exists.
// Phase 2: send initialize handshake if not yet initialized.
func (sm *ServerManager) ensureServerReady(ctx context.Context, name string, srv *ManagedMcpServer) error {
	// HTTP/SSE transport
	if srv.Config.Type == "sse" || srv.Config.Type == "http" {
		if srv.HTTPClient == nil {
			httpClient, err := NewHTTPClient(srv.Config.URL, srv.Config.Headers)
			if err != nil {
				return fmt.Errorf("HTTP client creation failed: %w", err)
			}
			srv.HTTPClient = httpClient
			srv.Initialized = false
		}
		if !srv.Initialized {
			if err := srv.HTTPClient.Initialize(ctx); err != nil {
				srv.HTTPClient.Close()
				srv.HTTPClient = nil
				return fmt.Errorf("HTTP initialize failed: %w", err)
			}
			srv.Initialized = true
		}
		return nil
	}

	// Stdio transport: Phase 1: spawn if no process
	if srv.Client == nil {
		client, err := NewClient(ctx, srv.Config.Command, srv.Config.Args, srv.Config.Env)
		if err != nil {
			return fmt.Errorf("spawn failed: %w", err)
		}
		srv.Client = client
		srv.Initialized = false
	}

	// Phase 2: initialize if not yet done
	if !srv.Initialized {
		if err := srv.Client.Initialize(ctx); err != nil {
			// Reset on failure so next call retries
			srv.Client.Close()
			srv.Client = nil
			return fmt.Errorf("initialize failed: %w", err)
		}
		srv.Initialized = true
	}

	return nil
}

// clearRoutesForServer removes all tool index entries for a given server.
func (sm *ServerManager) clearRoutesForServer(serverName string) {
	prefix := "mcp__" + serverName + "__"
	for k := range sm.toolIndex {
		if strings.HasPrefix(k, prefix) {
			delete(sm.toolIndex, k)
		}
	}
}

// MCPQualifiedName builds a qualified tool name from server and raw names.
// Format: mcp__<server>__<tool>
func MCPQualifiedName(serverName, toolName string) string {
	return "mcp__" + serverName + "__" + toolName
}
