package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// JSON-RPC 2.0 types for LSP communication.

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
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

type jsonRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// LSP Types

type InitializeParams struct {
	ProcessID    int               `json:"processId"`
	RootURI      string            `json:"rootUri,omitempty"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

type ClientCapabilities struct {
	TextDocument  interface{} `json:"textDocument,omitempty"`
	Workspace     interface{} `json:"workspace,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo,omitempty"`
}

type ServerCapabilities struct {
	TextDocumentSync  interface{} `json:"textDocumentSync,omitempty"`
	HoverProvider     bool        `json:"hoverProvider,omitempty"`
	DefinitionProvider bool       `json:"definitionProvider,omitempty"`
	ReferencesProvider bool       `json:"referencesProvider,omitempty"`
	RenameProvider    interface{} `json:"renameProvider,omitempty"`
	CompletionProvider interface{} `json:"completionProvider,omitempty"`
	DiagnosticProvider interface{} `json:"diagnosticProvider,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type Location struct {
	URI   string   `json:"uri"`
	Range Range    `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Range    Range   `json:"range"`
	Severity int     `json:"severity,omitempty"`
	Source   string  `json:"source,omitempty"`
	Message  string  `json:"message"`
}

type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// LocationLink represents an LSP LocationLink (link target with spans).
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange"`
	TargetUri            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// Client is an LSP client that communicates over stdio JSON-RPC.
type Client struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	idCounter  atomic.Int64
	pending    map[int64]chan jsonRPCResponse
	mu         sync.Mutex
	caps       ServerCapabilities
	serverInfo ServerInfo

	diagnostics map[string][]Diagnostic
	diagMu      sync.Mutex
}

// NewClient starts an LSP server and creates a client.
func NewClient(ctx context.Context, command string, args []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start LSP server: %w", err)
	}

	client := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReaderSize(stdout, 1024*1024),
		pending:     make(map[int64]chan jsonRPCResponse),
		diagnostics: make(map[string][]Diagnostic),
	}

	go client.readLoop()

	return client, nil
}

// Initialize sends the LSP initialize request.
func (c *Client) Initialize(ctx context.Context, rootURI string) (*InitializeResult, error) {
	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: map[string]interface{}{},
			Workspace:    map[string]interface{}{},
		},
	}

	resp, err := c.call("initialize", params)
	if err != nil {
		return nil, err
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse initialize result: %w", err)
	}

	c.caps = result.Capabilities
	c.serverInfo = result.ServerInfo

	// Send initialized notification
	c.notify("initialized", map[string]interface{}{})

	return &result, nil
}

// Shutdown sends the shutdown request.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.call("shutdown", nil)
	c.notify("exit", nil)
	return err
}

// Hover sends a textDocument/hover request.
func (c *Client) Hover(ctx context.Context, uri string, line, character int) (string, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}

	resp, err := c.call("textDocument/hover", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Contents interface{} `json:"contents"`
		Range    *Range      `json:"range,omitempty"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(result.Contents, "", "  ")
	return string(data), nil
}

// Definition sends a textDocument/definition request.
func (c *Client) Definition(ctx context.Context, uri string, line, character int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}

	resp, err := c.call("textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	// Can be Location, []Location, LocationLink, or []LocationLink.
	// Try LocationLink first (detected by presence of "targetUri" field).
	var locations []Location

	// Check if the result contains LocationLink objects (have "targetUri").
	raw := resp.Result
	if len(raw) > 0 {
		// Try as []LocationLink
		var links []LocationLink
		if err := json.Unmarshal(raw, &links); err == nil && len(links) > 0 && links[0].TargetUri != "" {
			for _, ll := range links {
				locations = append(locations, Location{
					URI:   ll.TargetUri,
					Range: ll.TargetSelectionRange,
				})
			}
			return locations, nil
		}

		// Try as single LocationLink
		var link LocationLink
		if err := json.Unmarshal(raw, &link); err == nil && link.TargetUri != "" {
			return []Location{{
				URI:   link.TargetUri,
				Range: link.TargetSelectionRange,
			}}, nil
		}
	}

	// Try as []Location
	if err := json.Unmarshal(resp.Result, &locations); err != nil {
		// Try single Location
		var loc Location
		if err := json.Unmarshal(resp.Result, &loc); err != nil {
			return nil, err
		}
		locations = []Location{loc}
	}
	return locations, nil
}

// References sends a textDocument/references request.
func (c *Client) References(ctx context.Context, uri string, line, character int, includeDecl bool) ([]Location, error) {
	params := map[string]interface{}{
		"textDocument": TextDocumentIdentifier{URI: uri},
		"position":     Position{Line: line, Character: character},
		"context": map[string]interface{}{
			"includeDeclaration": includeDecl,
		},
	}

	resp, err := c.call("textDocument/references", params)
	if err != nil {
		return nil, err
	}

	var locations []Location
	if err := json.Unmarshal(resp.Result, &locations); err != nil {
		return nil, err
	}
	return locations, nil
}

// Close shuts down the LSP client.
func (c *Client) Close() error {
	c.Shutdown(context.Background())
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

	// LSP uses Content-Length header
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
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
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.stdin.Write([]byte(header))
	c.stdin.Write(data)
}

func (c *Client) readLoop() {
	for {
		// Read Content-Length header
		var contentLength int
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = line[:len(line)-2] // strip \r\n
			if line == "" {
				break
			}
			if len(line) > 16 && line[:16] == "Content-Length: " {
				fmt.Sscanf(line[16:], "%d", &contentLength)
			}
		}

		if contentLength <= 0 {
			continue
		}

		body := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}

		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- resp
			}
			c.mu.Unlock()
		} else {
			// Handle notifications
			var notif struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(body, &notif); err == nil && notif.Method == "textDocument/publishDiagnostics" {
				var params struct {
					URI         string       `json:"uri"`
					Diagnostics []Diagnostic `json:"diagnostics"`
				}
				if err := json.Unmarshal(notif.Params, &params); err == nil {
					c.diagMu.Lock()
					c.diagnostics[params.URI] = params.Diagnostics
					c.diagMu.Unlock()
				}
			}
		}
	}
}

// GetDiagnostics returns the stored diagnostics for a given URI.
func (c *Client) GetDiagnostics(uri string) []Diagnostic {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	return c.diagnostics[uri]
}

// AllDiagnostics returns all stored diagnostics keyed by URI.
func (c *Client) AllDiagnostics() map[string][]Diagnostic {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	out := make(map[string][]Diagnostic, len(c.diagnostics))
	for k, v := range c.diagnostics {
		out[k] = v
	}
	return out
}
