package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/config"
)

// --- JSON-RPC 2.0 Types ---

// JsonRpcId mirrors Rust JsonRpcId.
type JsonRpcId struct {
	Number *uint64
	Str    *string
	IsNull bool
}

func (id JsonRpcId) MarshalJSON() ([]byte, error) {
	if id.IsNull {
		return []byte("null"), nil
	}
	if id.Number != nil {
		return []byte(strconv.FormatUint(*id.Number, 10)), nil
	}
	if id.Str != nil {
		return json.Marshal(*id.Str)
	}
	return []byte("null"), nil
}

func (id *JsonRpcId) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" {
		id.IsNull = true
		return nil
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		id.Number = &n
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		id.Str = &str
		return nil
	}
	return fmt.Errorf("invalid JSON-RPC id: %s", s)
}

func nextJsonRpcId(counter *uint64) JsonRpcId {
	n := *counter
	*counter++
	return JsonRpcId{Number: &n}
}

// JsonRpcRequest is a generic JSON-RPC 2.0 request.
type JsonRpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JsonRpcId       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JsonRpcError represents a JSON-RPC error object.
type JsonRpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JsonRpcError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// JsonRpcResponse is a generic JSON-RPC 2.0 response.
type JsonRpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JsonRpcId       `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JsonRpcError   `json:"error,omitempty"`
}

// --- MCP Protocol Types ---

// McpInitializeParams mirrors Rust McpInitializeParams.
type McpInitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      McpInitializeClientInfo `json:"clientInfo"`
}

// McpInitializeClientInfo mirrors Rust McpInitializeClientInfo.
type McpInitializeClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// McpInitializeResult mirrors Rust McpInitializeResult.
type McpInitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      McpInitializeServerInfo `json:"serverInfo"`
}

// McpInitializeServerInfo mirrors Rust McpInitializeServerInfo.
type McpInitializeServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// McpListToolsParams mirrors Rust McpListToolsParams.
type McpListToolsParams struct {
	Cursor *string `json:"cursor,omitempty"`
}

// McpTool mirrors Rust McpTool.
type McpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
}

// McpListToolsResult mirrors Rust McpListToolsResult.
type McpListToolsResult struct {
	Tools       []McpTool `json:"tools"`
	NextCursor  *string   `json:"nextCursor,omitempty"`
}

// McpToolCallParams mirrors Rust McpToolCallParams.
type McpToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Meta      map[string]interface{} `json:"_meta,omitempty"`
}

// McpToolCallContent mirrors Rust McpToolCallContent.
type McpToolCallContent struct {
	Kind string                 `json:"type"`
	Data map[string]interface{} `json:"-"`
}

// McpToolCallResult mirrors Rust McpToolCallResult.
type McpToolCallResult struct {
	Content           []McpToolCallContent   `json:"content"`
	StructuredContent map[string]interface{} `json:"structuredContent,omitempty"`
	IsError           *bool                  `json:"isError,omitempty"`
	Meta              map[string]interface{} `json:"_meta,omitempty"`
}

// McpListResourcesParams mirrors Rust McpListResourcesParams.
type McpListResourcesParams struct {
	Cursor *string `json:"cursor,omitempty"`
}

// McpResource mirrors Rust McpResource.
type McpResource struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mimeType,omitempty"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
}

// McpListResourcesResult mirrors Rust McpListResourcesResult.
type McpListResourcesResult struct {
	Resources  []McpResource `json:"resources"`
	NextCursor *string       `json:"nextCursor,omitempty"`
}

// McpReadResourceParams mirrors Rust McpReadResourceParams.
type McpReadResourceParams struct {
	URI string `json:"uri"`
}

// McpResourceContents mirrors Rust McpResourceContents.
type McpResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// McpReadResourceResult mirrors Rust McpReadResourceResult.
type McpReadResourceResult struct {
	Contents []McpResourceContents `json:"contents"`
}

// ManagedMcpTool mirrors Rust ManagedMcpTool.
type ManagedMcpTool struct {
	ServerName   string
	QualifiedName string
	RawName      string
	Tool         McpTool
}

// UnsupportedMcpServer mirrors Rust UnsupportedMcpServer.
type UnsupportedMcpServer struct {
	ServerName string
	Transport  string
	Reason     string
}

// McpServerManagerError mirrors Rust McpServerManagerError.
type McpServerManagerError struct {
	Kind    string // "io", "jsonrpc", "invalid_response", "unknown_tool", "unknown_server"
	Message string
	Cause   error
}

func (e *McpServerManagerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func mcpManagerIOErr(msg string, err error) *McpServerManagerError {
	return &McpServerManagerError{Kind: "io", Message: msg, Cause: err}
}

func mcpManagerJsonRpcErr(server, method string, rpcErr *JsonRpcError) *McpServerManagerError {
	return &McpServerManagerError{
		Kind:    "jsonrpc",
		Message: fmt.Sprintf("server %s method %s: %v", server, method, rpcErr),
	}
}

func mcpManagerInvalidResponseErr(server, method, details string) *McpServerManagerError {
	return &McpServerManagerError{Kind: "invalid_response", Message: fmt.Sprintf("server %s method %s: %s", server, method, details)}
}

func mcpManagerUnknownToolErr(name string) *McpServerManagerError {
	return &McpServerManagerError{Kind: "unknown_tool", Message: fmt.Sprintf("unknown tool: %s", name)}
}

func mcpManagerUnknownServerErr(name string) *McpServerManagerError {
	return &McpServerManagerError{Kind: "unknown_server", Message: fmt.Sprintf("unknown server: %s", name)}
}

// --- McpStdioProcess ---

// McpStdioProcess mirrors Rust McpStdioProcess — manages a child process with piped stdin/stdout.
type McpStdioProcess struct {
	cmd       *exec.Cmd
	stdin     *bufio.Writer
	stdinRaw  io.WriteCloser
	stdout    *bufio.Reader
	mu        sync.Mutex
}

// SpawnMcpStdioProcess spawns a new MCP stdio child process.
// Mirrors Rust McpStdioProcess::spawn.
func SpawnMcpStdioProcess(transport McpStdioTransport) (*McpStdioProcess, error) {
	cmd := exec.Command(transport.Command, transport.Args...)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	// Stderr goes to parent's stderr
	cmd.Stderr = os.Stderr

	// Apply environment variables
	if len(transport.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range transport.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP process %s: %w", transport.Command, err)
	}

	return &McpStdioProcess{
		cmd:      cmd,
		stdin:    bufio.NewWriter(stdinPipe),
		stdinRaw: stdinPipe,
		stdout:   bufio.NewReaderSize(stdoutPipe, 4096),
	}, nil
}

// WriteFrame writes a Content-Length framed message to the process stdin.
// Mirrors Rust write_frame.
func (p *McpStdioProcess) WriteFrame(payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := p.stdin.Write([]byte(header)); err != nil {
		return err
	}
	if _, err := p.stdin.Write(payload); err != nil {
		return err
	}
	return p.stdin.Flush() //nolint:errcheck // Flush for WriteCloser
}

// ReadFrame reads a Content-Length framed message from the process stdout.
// Mirrors Rust read_frame.
func (p *McpStdioProcess) ReadFrame() ([]byte, error) {
	var contentLength int
	for {
		line, err := p.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading frame header: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			// End of headers
			break
		}
		if strings.HasPrefix(trimmed, "Content-Length:") {
			lenStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "Content-Length:"))
			n, err := strconv.Atoi(lenStr)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", lenStr)
			}
			contentLength = n
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(p.stdout, buf); err != nil {
		return nil, fmt.Errorf("reading frame payload (%d bytes): %w", contentLength, err)
	}
	return buf, nil
}

// WriteJSONRPCMessage serializes and writes a JSON-RPC message with framing.
// Mirrors Rust write_jsonrpc_message.
func (p *McpStdioProcess) WriteJSONRPCMessage(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSON-RPC message: %w", err)
	}
	return p.WriteFrame(data)
}

// ReadJSONRPCMessage reads and deserializes a framed JSON-RPC message.
// Mirrors Rust read_jsonrpc_message.
func (p *McpStdioProcess) ReadJSONRPCMessage(v interface{}) error {
	frame, err := p.ReadFrame()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(frame, v); err != nil {
		return fmt.Errorf("unmarshal JSON-RPC message: %w (raw: %s)", err, string(frame[:min(len(frame), 200)]))
	}
	return nil
}

// SendRequest sends a JSON-RPC request.
func (p *McpStdioProcess) SendRequest(req *JsonRpcRequest) error {
	return p.WriteJSONRPCMessage(req)
}

// ReadResponse reads a JSON-RPC response.
func (p *McpStdioProcess) ReadResponse(resp *JsonRpcResponse) error {
	return p.ReadJSONRPCMessage(resp)
}

// Request sends a request and reads a response.
// Mirrors Rust McpStdioProcess::request.
func (p *McpStdioProcess) Request(id JsonRpcId, method string, params interface{}) (*JsonRpcResponse, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsJSON = data
	}
	req := &JsonRpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}
	if err := p.SendRequest(req); err != nil {
		return nil, fmt.Errorf("send request %s: %w", method, err)
	}

	resp := &JsonRpcResponse{}
	if err := p.ReadResponse(resp); err != nil {
		return nil, fmt.Errorf("read response for %s: %w", method, err)
	}
	return resp, nil
}

// Initialize sends an MCP initialize request.
// Mirrors Rust McpStdioProcess::initialize.
func (p *McpStdioProcess) Initialize(id JsonRpcId, params McpInitializeParams) (*McpInitializeResult, error) {
	resp, err := p.Request(id, "initialize", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result McpInitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal initialize result: %w", err)
	}
	return &result, nil
}

// ListTools sends an MCP tools/list request with optional cursor.
// Mirrors Rust McpStdioProcess::list_tools.
func (p *McpStdioProcess) ListTools(id JsonRpcId, params *McpListToolsParams) (*McpListToolsResult, error) {
	resp, err := p.Request(id, "tools/list", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result McpListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal list_tools result: %w", err)
	}
	return &result, nil
}

// CallTool sends an MCP tools/call request.
// Mirrors Rust McpStdioProcess::call_tool.
func (p *McpStdioProcess) CallTool(id JsonRpcId, params McpToolCallParams) (*McpToolCallResult, error) {
	resp, err := p.Request(id, "tools/call", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result McpToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal call_tool result: %w", err)
	}
	return &result, nil
}

// ListResources sends an MCP resources/list request.
// Mirrors Rust McpStdioProcess::list_resources.
func (p *McpStdioProcess) ListResources(id JsonRpcId, params *McpListResourcesParams) (*McpListResourcesResult, error) {
	resp, err := p.Request(id, "resources/list", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result McpListResourcesResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal list_resources result: %w", err)
	}
	return &result, nil
}

// ReadResource sends an MCP resources/read request.
// Mirrors Rust McpStdioProcess::read_resource.
func (p *McpStdioProcess) ReadResource(id JsonRpcId, params McpReadResourceParams) (*McpReadResourceResult, error) {
	resp, err := p.Request(id, "resources/read", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result McpReadResourceResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal read_resource result: %w", err)
	}
	return &result, nil
}

// Terminate kills the child process.
func (p *McpStdioProcess) Terminate() error {
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the child process to exit.
func (p *McpStdioProcess) Wait() error {
	return p.cmd.Wait()
}

// Shutdown kills the process if running and waits for exit.
func (p *McpStdioProcess) Shutdown() error {
	if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
		return nil
	}
	if p.cmd.Process != nil {
		p.cmd.Process.Kill() //nolint:errcheck
	}
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for MCP process to exit")
	}
}

// --- ManagedMcpServer (internal) ---

type managedMcpServer struct {
	bootstrap    *McpClientBootstrap
	process      *McpStdioProcess
	initialized  bool
}

// --- toolRoute (internal) ---

type toolRoute struct {
	serverName string
	rawName    string
}

// --- McpServerManager ---

// McpServerManager manages multiple MCP stdio servers.
// Mirrors Rust McpServerManager.
type McpServerManager struct {
	mu                sync.Mutex
	servers           map[string]*managedMcpServer
	unsupportedServers []UnsupportedMcpServer
	toolIndex         map[string]toolRoute
	nextRequestID     uint64
}

// NewMcpServerManager creates a manager from MCP server configs.
// Mirrors Rust McpServerManager::from_servers.
// Accepts the backward-compatible MCPServer map from config.Config.
func NewMcpServerManager(servers map[string]config.MCPServer) *McpServerManager {
	mgr := &McpServerManager{
		servers:       make(map[string]*managedMcpServer),
		toolIndex:     make(map[string]toolRoute),
		nextRequestID: 1,
	}

	for name, cfg := range servers {
		transport := strings.ToLower(cfg.Type)
		if transport != "stdio" {
			mgr.unsupportedServers = append(mgr.unsupportedServers, UnsupportedMcpServer{
				ServerName: name,
				Transport:  cfg.Type,
				Reason:     fmt.Sprintf("unsupported transport type: %s (only stdio is supported)", cfg.Type),
			})
			continue
		}
		bootstrap := &McpClientBootstrap{
			ServerName: name,
			Transport: McpClientTransport{
				Type: "stdio",
				Stdio: &McpStdioTransport{
					Command: cfg.Command,
					Args:    cfg.Args,
					Env:     cfg.Env,
				},
			},
		}
		mgr.servers[name] = &managedMcpServer{
			bootstrap:   bootstrap,
			initialized: false,
		}
	}

	// Sort unsupported for deterministic output
	sort.Slice(mgr.unsupportedServers, func(i, j int) bool {
		return mgr.unsupportedServers[i].ServerName < mgr.unsupportedServers[j].ServerName
	})

	return mgr
}

// UnsupportedServers returns the list of unsupported servers.
func (m *McpServerManager) UnsupportedServers() []UnsupportedMcpServer {
	return m.unsupportedServers
}

// DiscoverTools discovers all tools from all managed MCP servers.
// Mirrors Rust McpServerManager::discover_tools.
func (m *McpServerManager) DiscoverTools() ([]ManagedMcpTool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var allTools []ManagedMcpTool
	var errs []string

	// Collect server names in sorted order for determinism
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		server := m.servers[name]

		// Ensure the server is ready (spawned + initialized)
		if err := m.ensureServerReady(server); err != nil {
			errs = append(errs, fmt.Sprintf("server %s: %v", name, err))
			continue
		}

		// Clear existing routes for this server
		m.clearRoutesForServer(name)

		// Paginated tool discovery
		var cursor *string
		for {
			id := m.takeRequestID()
			result, err := server.process.ListTools(id, &McpListToolsParams{Cursor: cursor})
			if err != nil {
				errs = append(errs, fmt.Sprintf("server %s list_tools: %v", name, err))
				break
			}

			for _, tool := range result.Tools {
				qualifiedName := MCPToolName(name, tool.Name)
				m.toolIndex[qualifiedName] = toolRoute{
					serverName: name,
					rawName:    tool.Name,
				}
				allTools = append(allTools, ManagedMcpTool{
					ServerName:    name,
					QualifiedName: qualifiedName,
					RawName:       tool.Name,
					Tool:          tool,
				})
			}

			if result.NextCursor == nil {
				break
			}
			cursor = result.NextCursor
		}
	}

	if len(errs) > 0 && len(allTools) == 0 {
		return nil, fmt.Errorf("tool discovery failed: %s", strings.Join(errs, "; "))
	}

	return allTools, nil
}

// CallTool calls a tool by its qualified name.
// Mirrors Rust McpServerManager::call_tool.
func (m *McpServerManager) CallTool(qualifiedToolName string, arguments map[string]interface{}) (*McpToolCallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	route, ok := m.toolIndex[qualifiedToolName]
	if !ok {
		return nil, mcpManagerUnknownToolErr(qualifiedToolName)
	}

	server, ok := m.servers[route.serverName]
	if !ok {
		return nil, mcpManagerUnknownServerErr(route.serverName)
	}

	if err := m.ensureServerReady(server); err != nil {
		return nil, err
	}

	id := m.takeRequestID()
	params := McpToolCallParams{
		Name:      route.rawName,
		Arguments: arguments,
	}

	result, err := server.process.CallTool(id, params)
	if err != nil {
		return nil, fmt.Errorf("call tool %s on server %s: %w", qualifiedToolName, route.serverName, err)
	}

	return result, nil
}

// ListResources discovers resources from a specific server.
func (m *McpServerManager) ListResources(serverName string) ([]McpResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, ok := m.servers[serverName]
	if !ok {
		return nil, mcpManagerUnknownServerErr(serverName)
	}

	if err := m.ensureServerReady(server); err != nil {
		return nil, err
	}

	id := m.takeRequestID()
	result, err := server.process.ListResources(id, nil)
	if err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// ReadResource reads a resource from a specific server.
func (m *McpServerManager) ReadResource(serverName, uri string) ([]McpResourceContents, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, ok := m.servers[serverName]
	if !ok {
		return nil, mcpManagerUnknownServerErr(serverName)
	}

	if err := m.ensureServerReady(server); err != nil {
		return nil, err
	}

	id := m.takeRequestID()
	result, err := server.process.ReadResource(id, McpReadResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}

	return result.Contents, nil
}

// Shutdown shuts down all managed MCP server processes.
// Mirrors Rust McpServerManager::shutdown.
func (m *McpServerManager) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for name, server := range m.servers {
		if server.process != nil {
			if err := server.process.Shutdown(); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			}
			server.process = nil
			server.initialized = false
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- Private helpers ---

func (m *McpServerManager) takeRequestID() JsonRpcId {
	return nextJsonRpcId(&m.nextRequestID)
}

func (m *McpServerManager) clearRoutesForServer(serverName string) {
	for qualified, route := range m.toolIndex {
		if route.serverName == serverName {
			delete(m.toolIndex, qualified)
		}
	}
}

func (m *McpServerManager) ensureServerReady(server *managedMcpServer) error {
	// Spawn the process if not running
	if server.process == nil {
		if server.bootstrap.Transport.Stdio == nil {
			return fmt.Errorf("server %s: no stdio transport configured", server.bootstrap.ServerName)
		}
		proc, err := SpawnMcpStdioProcess(*server.bootstrap.Transport.Stdio)
		if err != nil {
			return fmt.Errorf("spawn MCP process: %w", err)
		}
		server.process = proc
		server.initialized = false
	}

	// Initialize if not yet done
	if !server.initialized {
		id := m.takeRequestID()
		params := defaultInitializeParams()
		_, err := server.process.Initialize(id, params)
		if err != nil {
			// Clean up the failed process
			server.process.Shutdown() //nolint:errcheck
			server.process = nil
			return fmt.Errorf("initialize MCP server: %w", err)
		}
		server.initialized = true
	}

	return nil
}

func defaultInitializeParams() McpInitializeParams {
	return McpInitializeParams{
		ProtocolVersion: "2025-03-26",
		Capabilities:    map[string]interface{}{},
		ClientInfo: McpInitializeClientInfo{
			Name:    "claw",
			Version: "0.4.0",
		},
	}
}

// --- Encoding helpers ---

func encodeFrame(payload []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(payload))
	buf.Write(payload)
	return buf.Bytes()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
