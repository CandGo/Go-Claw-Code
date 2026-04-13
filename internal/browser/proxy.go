package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultProxyPort is the default CDP proxy HTTP port.
const DefaultProxyPort = 3456

// --- CDP Protocol Types ---

type cdpRequest struct {
	ID        int64       `json:"id"`
	Method    string      `json:"method"`
	Params    interface{} `json:"params,omitempty"`
	SessionID string      `json:"sessionId,omitempty"`
}

type cdpResponse struct {
	ID     int64            `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *cdpError        `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cdpEvent is an incoming CDP event (no ID).
type cdpEvent struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
}

// --- CDPProxy ---

// CDPProxy maintains a persistent raw WebSocket connection to Chrome
// and exposes an HTTP API, 1:1 replicating eze-is/web-access.
type CDPProxy struct {
	port   int
	server *http.Server

	// Chrome WebSocket connection
	wsConn *websocket.Conn
	wsMu   sync.Mutex // serialize WS writes
	cmdID  int64
	pending map[int64]chan *cdpResponse // id → response channel
	pendMu  sync.Mutex

	// Session management: targetId → sessionId
	sessions    map[string]string
	sessionsMu  sync.RWMutex

	// Port guard: sessionId → bool (Fetch.enable active)
	portGuarded   map[string]bool
	portGuardedMu sync.Mutex

	// Chrome discovery state
	chromePort int
	wsPath     string
	mu         sync.RWMutex

	// User-Agent override (optional extension)
	userAgent string

	// Auto-exit: last activity timestamp
	lastActivity atomic.Int64
}

// defaultProxy holds the singleton proxy instance.
var defaultProxy *CDPProxy

// NewCDPProxy creates a new CDP proxy.
func NewCDPProxy(port int) *CDPProxy {
	if port <= 0 {
		port = DefaultProxyPort
	}
	return &CDPProxy{
		port:        port,
		pending:     make(map[int64]chan *cdpResponse),
		sessions:    make(map[string]string),
		portGuarded: make(map[string]bool),
	}
}

// ProxyPort returns the configured proxy port from env or default.
func ProxyPort() int {
	if p := os.Getenv("CLAW_PROXY_PORT"); p != "" {
		var port int
		fmt.Sscanf(p, "%d", &port)
		if port > 0 {
			return port
		}
	}
	return DefaultProxyPort
}

// IsProxyRunning checks if a CDP proxy is already running on the given port.
func IsProxyRunning(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// Start starts the HTTP server and connects to Chrome.
func (p *CDPProxy) Start() error {
	// Connect to Chrome (non-blocking -- will retry on demand)
	go func() {
		if err := p.connect(); err != nil {
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] initial connection failed: %v (will retry on demand)\n", err)
		}
	}()

	p.lastActivity.Store(time.Now().Unix())

	mux := http.NewServeMux()
	mux.HandleFunc("/health", p.trackActivity(p.handleHealth))
	mux.HandleFunc("/targets", p.trackActivity(p.handleTargets))
	mux.HandleFunc("/new", p.trackActivity(p.handleNew))
	mux.HandleFunc("/close", p.trackActivity(p.handleClose))
	mux.HandleFunc("/navigate", p.trackActivity(p.handleNavigate))
	mux.HandleFunc("/back", p.trackActivity(p.handleBack))
	mux.HandleFunc("/info", p.trackActivity(p.handleInfo))
	mux.HandleFunc("/eval", p.trackActivity(p.handleEval))
	mux.HandleFunc("/click", p.trackActivity(p.handleClick))
	mux.HandleFunc("/clickAt", p.trackActivity(p.handleClickAt))
	mux.HandleFunc("/setFiles", p.trackActivity(p.handleSetFiles))
	mux.HandleFunc("/scroll", p.trackActivity(p.handleScroll))
	mux.HandleFunc("/screenshot", p.trackActivity(p.handleScreenshot))
	mux.HandleFunc("/set_ua", p.trackActivity(p.handleSetUA))

	addr := fmt.Sprintf("127.0.0.1:%d", p.port)
	p.server = &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] HTTP server error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "  [cdp-proxy] listening on http://%s\n", addr)
	return nil
}

// Stop shuts down the proxy.
func (p *CDPProxy) Stop() {
	if p.server != nil {
		p.server.Close()
	}
	p.closeWS()
}

// --- Chrome Connection ---

func (p *CDPProxy) closeWS() {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	if p.wsConn != nil {
		p.wsConn.Close()
		p.wsConn = nil
	}
}

// connect discovers and connects to Chrome.
// Does NOT hold p.mu during WS dial (which can take 120s for approval).
func (p *CDPProxy) connect() error {
	// Quick check under read lock
	p.mu.RLock()
	hasConn := p.wsConn != nil
	p.mu.RUnlock()
	if hasConn {
		return nil
	}

	// Clear stale sessions
	p.sessionsMu.Lock()
	p.sessions = make(map[string]string)
	p.sessionsMu.Unlock()
	p.portGuardedMu.Lock()
	p.portGuarded = make(map[string]bool)
	p.portGuardedMu.Unlock()

	// Try strategies without holding the lock (WS dial can block 120s)
	type connectResult struct {
		conn       *websocket.Conn
		chromePort int
		wsURL      string
	}

	tryConn := func(wsURL string) (*connectResult, error) {
		dialer := websocket.Dialer{
			HandshakeTimeout: 120 * time.Second, // Wait for Chrome approval dialog
		}
		conn, resp, err := dialer.Dial(wsURL, nil)
		if err != nil {
			statusInfo := ""
			if resp != nil {
				statusInfo = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
			}
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] WS dial %s failed: %v%s\n", wsURL, err, statusInfo)
			return nil, fmt.Errorf("WS dial failed: %w", err)
		}
		return &connectResult{conn: conn, wsURL: wsURL}, nil
	}

	var result *connectResult

	// Strategy 0: Cached URL
	if cached := loadWSURLCache(); cached != "" {
		fmt.Fprintf(os.Stderr, "  [cdp-proxy] trying cached URL...\n")
		if r, err := tryConn(cached); err == nil {
			result = r
		} else {
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] cached URL stale\n")
		}
	}

	// Strategy 1: DevToolsActivePort
	if result == nil {
		if wsURL, err := readDevToolsActivePort(); err == nil && wsURL != "" {
			port := extractPortFromURL(wsURL)
			if port > 0 && isPortOpen("127.0.0.1", port) {
				fmt.Fprintf(os.Stderr, "  [cdp-proxy] DevToolsActivePort port %d reachable (TCP OK)\n", port)
				if r, err := tryConn(wsURL); err == nil {
					result = r
					result.chromePort = port
				} else {
					fmt.Fprintf(os.Stderr, "  [cdp-proxy] WS connect to DevToolsActivePort failed\n")
				}
			}
		}
	}

	// Strategy 2: HTTP discovery on common ports
	if result == nil {
		for _, port := range []int{9222, 9229, 9333} {
			if !isPortOpen("127.0.0.1", port) {
				continue
			}
			url := fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser", port)
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] trying port %d\n", port)
			if r, err := tryConn(url); err == nil {
				result = r
				result.chromePort = port
				break
			}
		}
	}

	// Strategy 3: Auto-launch Chrome with --remote-debugging-port
	if result == nil {
		fmt.Fprintf(os.Stderr, "  [cdp-proxy] Chrome not found, auto-launching with debug port...\n")
		if chromePath := findChrome(); chromePath != "" {
			if err := launchChromeWithDebugPort(chromePath, 9222); err != nil {
				fmt.Fprintf(os.Stderr, "  [cdp-proxy] auto-launch failed: %v\n", err)
			} else {
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					if isPortOpen("127.0.0.1", 9222) {
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				url := fmt.Sprintf("ws://127.0.0.1:9222/devtools/browser")
				if r, err := tryConn(url); err == nil {
					result = r
					result.chromePort = 9222
				}
			}
		}
	}

	if result == nil {
		return fmt.Errorf("Chrome not found \xe2\x80\x94 start Chrome with --remote-debugging-port=9222 or update the Chrome desktop shortcut")
	}

	// Now acquire lock to store the connection
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check: another goroutine might have connected already
	if p.wsConn != nil {
		result.conn.Close()
		return nil
	}

	p.wsConn = result.conn
	p.pending = make(map[int64]chan *cdpResponse)
	p.chromePort = result.chromePort

	// Start read loop
	go p.readLoop()

	saveWSURLCache(result.wsURL)
	fmt.Fprintf(os.Stderr, "  [cdp-proxy] connected: %s\n", result.wsURL)
	return nil
}

// ensureConnected checks if WS is alive, reconnects if needed.
func (p *CDPProxy) ensureConnected() error {
	p.mu.RLock()
	hasConn := p.wsConn != nil
	p.mu.RUnlock()
	if hasConn {
		return nil
	}
	return p.connect()
}

// --- CDP Protocol Layer ---

// readLoop reads WebSocket messages and dispatches to pending channels or handles events.
func (p *CDPProxy) readLoop() {
	for {
		p.mu.RLock()
		conn := p.wsConn
		p.mu.RUnlock()
		if conn == nil {
			return
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [cdp-proxy] WS read error: %v\n", err)
			// Connection lost -- clear state
			p.closeWS()
			p.sessionsMu.Lock()
			p.sessions = make(map[string]string)
			p.sessionsMu.Unlock()
			return
		}

		// Parse message
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(msg, &raw); err != nil {
			continue
		}

		// Check if it's a response (has "id") or event (has "method")
		if _, hasID := raw["id"]; hasID {
			var resp cdpResponse
			if err := json.Unmarshal(msg, &resp); err != nil {
				continue
			}
			p.pendMu.Lock()
			if ch, ok := p.pending[resp.ID]; ok {
				ch <- &resp
				delete(p.pending, resp.ID)
			}
			p.pendMu.Unlock()
		} else if _, hasMethod := raw["method"]; hasMethod {
			var evt cdpEvent
			if err := json.Unmarshal(msg, &evt); err != nil {
				continue
			}
			p.handleEvent(&evt)
		}
	}
}

// handleEvent processes incoming CDP events.
func (p *CDPProxy) handleEvent(evt *cdpEvent) {
	switch evt.Method {
	case "Target.attachedToTarget":
		var params struct {
			SessionID   string `json:"sessionId"`
			TargetInfo  struct {
				TargetID string `json:"targetId"`
			} `json:"targetInfo"`
		}
		if err := json.Unmarshal(evt.Params, &params); err == nil {
			p.sessionsMu.Lock()
			p.sessions[params.TargetInfo.TargetID] = params.SessionID
			p.sessionsMu.Unlock()
		}

	case "Fetch.requestPaused":
		// Anti-detection: fail requests to Chrome debug port
		var params struct {
			RequestID string `json:"requestId"`
			SessionID string `json:"sessionId"` // not in params, but we have it from evt
		}
		if err := json.Unmarshal(evt.Params, &params); err == nil {
			sid := params.SessionID
			if sid == "" {
				sid = evt.SessionID
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p.sendCDPContext(ctx, "Fetch.failRequest", map[string]interface{}{
				"requestId":   params.RequestID,
				"errorReason": "ConnectionRefused",
			}, sid)
		}
	}
}

// sendCDP sends a CDP command and waits for response.
func (p *CDPProxy) sendCDP(method string, params interface{}, sessionID string) (*cdpResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return p.sendCDPContext(ctx, method, params, sessionID)
}

// sendCDPContext sends a CDP command with context-based timeout.
func (p *CDPProxy) sendCDPContext(ctx context.Context, method string, params interface{}, sessionID string) (*cdpResponse, error) {
	p.mu.RLock()
	conn := p.wsConn
	p.mu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("not connected to Chrome")
	}

	id := p.nextCmdID()
	req := cdpRequest{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	}

	// Register pending response channel
	ch := make(chan *cdpResponse, 1)
	p.pendMu.Lock()
	p.pending[id] = ch
	p.pendMu.Unlock()
	defer func() {
		p.pendMu.Lock()
		delete(p.pending, id)
		p.pendMu.Unlock()
	}()

	// Send
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal CDP request: %w", err)
	}
	p.wsMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	p.wsMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write WS: %w", err)
	}

	// Wait for response or timeout
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp, fmt.Errorf("CDP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("CDP timeout: %s", method)
	}
}

func (p *CDPProxy) nextCmdID() int64 {
	p.pendMu.Lock()
	defer p.pendMu.Unlock()
	p.cmdID++
	return p.cmdID
}

// --- Session Management ---

// ensureSession attaches to a target if not already attached.
func (p *CDPProxy) ensureSession(targetID string) (string, error) {
	if err := p.ensureConnected(); err != nil {
		return "", err
	}

	p.sessionsMu.RLock()
	if sid, ok := p.sessions[targetID]; ok {
		p.sessionsMu.RUnlock()
		return sid, nil
	}
	p.sessionsMu.RUnlock()

	// Attach
	resp, err := p.sendCDP("Target.attachToTarget", map[string]interface{}{
		"targetId": targetID,
		"flatten":  true,
	}, "")
	if err != nil {
		return "", err
	}

	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", err
	}
	if result.SessionID == "" {
		return "", fmt.Errorf("attach returned empty sessionId")
	}

	p.sessionsMu.Lock()
	p.sessions[targetID] = result.SessionID
	p.sessionsMu.Unlock()

	// Enable port guard
	p.enablePortGuard(result.SessionID)

	return result.SessionID, nil
}

// enablePortGuard intercepts requests from pages to Chrome's debug port (anti-detection).
func (p *CDPProxy) enablePortGuard(sessionID string) {
	if p.chromePort <= 0 {
		return
	}
	p.portGuardedMu.Lock()
	if p.portGuarded[sessionID] {
		p.portGuardedMu.Unlock()
		return
	}
	p.portGuarded[sessionID] = true
	p.portGuardedMu.Unlock()

	patterns := []interface{}{
		map[string]interface{}{
			"urlPattern":  fmt.Sprintf("http://127.0.0.1:%d/*", p.chromePort),
			"requestStage": "Request",
		},
		map[string]interface{}{
			"urlPattern":  fmt.Sprintf("http://localhost:%d/*", p.chromePort),
			"requestStage": "Request",
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.sendCDPContext(ctx, "Fetch.enable", map[string]interface{}{"patterns": patterns}, sessionID)
}

// waitForLoad waits for page to reach "complete" readyState.
func (p *CDPProxy) waitForLoad(sessionID string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Enable Page domain
	p.sendCDPContext(ctx, "Page.enable", nil, sessionID)

	// Poll readyState
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := p.sendCDPContext(ctx, "Runtime.evaluate", map[string]interface{}{
				"expression":  "document.readyState",
				"returnByValue": true,
			}, sessionID)
			if err != nil {
				return
			}
			var result struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(resp.Result, &result) == nil && result.Result.Value == "complete" {
				return
			}
		}
	}
}

// --- HTTP Helpers ---

func (p *CDPProxy) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(data)
}

func (p *CDPProxy) writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": msg})
}

func (p *CDPProxy) readBody(r *http.Request) string {
	data, _ := io.ReadAll(r.Body)
	return string(data)
}

// --- HTTP Handlers ---

func (p *CDPProxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	connected := p.wsConn != nil
	port := p.chromePort
	p.mu.RUnlock()
	p.sessionsMu.RLock()
	sessCount := len(p.sessions)
	p.sessionsMu.RUnlock()

	p.writeJSON(w, map[string]interface{}{
		"status":     "ok",
		"connected":  connected,
		"sessions":   sessCount,
		"chromePort": port,
	})
}

func (p *CDPProxy) handleTargets(w http.ResponseWriter, r *http.Request) {
	if err := p.ensureConnected(); err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}
	resp, err := p.sendCDP("Target.getTargets", nil, "")
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	var result struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			URL      string `json:"url"`
			Title    string `json:"title"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	json.Unmarshal(resp.Result, &result)

	// Filter page targets only
	var pages []map[string]string
	for _, t := range result.TargetInfos {
		if t.Type == "page" {
			pages = append(pages, map[string]string{
				"targetId": t.TargetID,
				"url":      t.URL,
				"title":    t.Title,
			})
		}
	}
	p.writeJSON(w, pages)
}

func (p *CDPProxy) handleNew(w http.ResponseWriter, r *http.Request) {
	if err := p.ensureConnected(); err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		targetURL = "about:blank"
	}

	resp, err := p.sendCDP("Target.createTarget", map[string]interface{}{
		"url":        targetURL,
		"background": true,
	}, "")
	if err != nil {
		p.writeErr(w, 500, "create target: "+err.Error())
		return
	}

	var result struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(resp.Result, &result)

	// Wait for page load if not blank
	if targetURL != "about:blank" {
		sid, err := p.ensureSession(result.TargetID)
		if err == nil {
			p.waitForLoad(sid, 15*time.Second)
		}
	}

	p.writeJSON(w, map[string]interface{}{"targetId": result.TargetID})
}

func (p *CDPProxy) handleClose(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	resp, err := p.sendCDP("Target.closeTarget", map[string]interface{}{
		"targetId": targetID,
	}, "")
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	// Clean up session
	p.sessionsMu.Lock()
	delete(p.sessions, targetID)
	p.sessionsMu.Unlock()

	p.writeJSON(w, resp.Result)
}

func (p *CDPProxy) handleNavigate(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	url := r.URL.Query().Get("url")
	if targetID == "" || url == "" {
		p.writeErr(w, 400, "target and url parameters required")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	resp, err := p.sendCDP("Page.navigate", map[string]interface{}{
		"url": url,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, "navigate: "+err.Error())
		return
	}

	p.waitForLoad(sid, 15*time.Second)
	p.writeJSON(w, resp.Result)
}

func (p *CDPProxy) handleBack(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression": "history.back()",
	}, sid)
	p.waitForLoad(sid, 15*time.Second)

	p.writeJSON(w, map[string]interface{}{"ok": true})
}

func (p *CDPProxy) handleInfo(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	resp, err := p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression":   "JSON.stringify({title: document.title, url: location.href, ready: document.readyState})",
		"returnByValue": true,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	json.Unmarshal(resp.Result, &result)

	if result.Result.Value != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(result.Result.Value))
		return
	}
	p.writeJSON(w, map[string]interface{}{})
}

func (p *CDPProxy) handleEval(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	expr := p.readBody(r)
	if expr == "" {
		expr = r.URL.Query().Get("expr")
	}
	if expr == "" {
		expr = "document.title"
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	resp, err := p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	var result struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	json.Unmarshal(resp.Result, &result)

	if result.ExceptionDetails != nil {
		p.writeErr(w, 400, result.ExceptionDetails.Text)
		return
	}

	if result.Result.Value != nil {
		p.writeJSON(w, map[string]interface{}{"value": result.Result.Value})
		return
	}
	p.writeJSON(w, resp.Result)
}

func (p *CDPProxy) handleClick(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	selector := p.readBody(r)
	if selector == "" {
		p.writeErr(w, 400, "POST body needs CSS selector")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	selectorJSON, _ := json.Marshal(selector)
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%s);
		if (!el) return { error: 'element not found: ' + %s };
		el.scrollIntoView({ block: 'center' });
		el.click();
		return { clicked: true, tag: el.tagName, text: (el.textContent || '').slice(0, 100) };
	})()`, string(selectorJSON), string(selectorJSON))

	resp, err := p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
		"awaitPromise":  true,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, "click: "+err.Error())
		return
	}

	var result struct {
		Result struct {
			Value map[string]interface{} `json:"value,omitempty"`
		} `json:"result"`
	}
	json.Unmarshal(resp.Result, &result)

	if val := result.Result.Value; val != nil {
		if errMsg, ok := val["error"].(string); ok {
			p.writeErr(w, 400, errMsg)
			return
		}
		p.writeJSON(w, val)
		return
	}
	p.writeJSON(w, resp.Result)
}

func (p *CDPProxy) handleClickAt(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	selector := p.readBody(r)
	if selector == "" {
		p.writeErr(w, 400, "POST body needs CSS selector")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	// Get element coordinates
	selectorJSON, _ := json.Marshal(selector)
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%s);
		if (!el) return { error: 'element not found: ' + %s };
		el.scrollIntoView({ block: 'center' });
		const rect = el.getBoundingClientRect();
		return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2, tag: el.tagName, text: (el.textContent || '').slice(0, 100) };
	})()`, string(selectorJSON), string(selectorJSON))

	resp, err := p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
		"awaitPromise":  true,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	var evalResult struct {
		Result struct {
			Value map[string]interface{} `json:"value,omitempty"`
		} `json:"result"`
	}
	json.Unmarshal(resp.Result, &evalResult)

	coord := evalResult.Result.Value
	if coord == nil {
		p.writeErr(w, 400, "could not get element coordinates")
		return
	}
	if errMsg, ok := coord["error"].(string); ok {
		p.writeErr(w, 400, errMsg)
		return
	}

	x, _ := coord["x"].(float64)
	y, _ := coord["y"].(float64)

	// Dispatch real mouse events via CDP
	p.sendCDP("Input.dispatchMouseEvent", map[string]interface{}{
		"type":       "mousePressed",
		"x":          x,
		"y":          y,
		"button":     "left",
		"clickCount": 1,
	}, sid)
	p.sendCDP("Input.dispatchMouseEvent", map[string]interface{}{
		"type":       "mouseReleased",
		"x":          x,
		"y":          y,
		"button":     "left",
		"clickCount": 1,
	}, sid)

	tag, _ := coord["tag"].(string)
	text, _ := coord["text"].(string)
	p.writeJSON(w, map[string]interface{}{
		"clicked": true,
		"x":       x,
		"y":       y,
		"tag":     tag,
		"text":    text,
	})
}

func (p *CDPProxy) handleSetFiles(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	var body struct {
		Selector string   `json:"selector"`
		Files    []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(p.readBody(r)), &body); err != nil {
		p.writeErr(w, 400, "invalid JSON body")
		return
	}
	if body.Selector == "" || len(body.Files) == 0 {
		p.writeErr(w, 400, "selector and files required")
		return
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	// Enable DOM domain
	p.sendCDP("DOM.enable", nil, sid)

	// Get document
	docResp, err := p.sendCDP("DOM.getDocument", nil, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}
	var docResult struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	json.Unmarshal(docResp.Result, &docResult)

	// Find element
	nodeResp, err := p.sendCDP("DOM.querySelector", map[string]interface{}{
		"nodeId":  docResult.Root.NodeID,
		"selector": body.Selector,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}
	var nodeResult struct {
		NodeID int `json:"nodeId"`
	}
	json.Unmarshal(nodeResp.Result, &nodeResult)

	if nodeResult.NodeID == 0 {
		p.writeErr(w, 400, "element not found: "+body.Selector)
		return
	}

	// Set files
	_, err = p.sendCDP("DOM.setFileInputFiles", map[string]interface{}{
		"nodeId": nodeResult.NodeID,
		"files":  body.Files,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	p.writeJSON(w, map[string]interface{}{"success": true, "files": len(body.Files)})
}

func (p *CDPProxy) handleScroll(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	y := 3000
	if yStr := r.URL.Query().Get("y"); yStr != "" {
		fmt.Sscanf(yStr, "%d", &y)
	}
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "down"
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	var js string
	switch direction {
	case "top":
		js = `window.scrollTo(0, 0); "scrolled to top"`
	case "bottom":
		js = `window.scrollTo(0, document.body.scrollHeight); "scrolled to bottom"`
	case "up":
		js = fmt.Sprintf(`window.scrollBy(0, -%d); "scrolled up %dpx"`, abs(y), abs(y))
	default: // down
		js = fmt.Sprintf(`window.scrollBy(0, %d); "scrolled down %dpx"`, abs(y), abs(y))
	}

	resp, err := p.sendCDP("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, sid)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	// Wait for lazy-load triggers
	time.Sleep(800 * time.Millisecond)

	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	json.Unmarshal(resp.Result, &result)
	p.writeJSON(w, map[string]interface{}{"value": result.Result.Value})
}

func (p *CDPProxy) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		p.writeErr(w, 400, "target parameter required")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "png"
	}

	sid, err := p.ensureSession(targetID)
	if err != nil {
		p.writeErr(w, 500, err.Error())
		return
	}

	params := map[string]interface{}{
		"format": format,
	}
	if format == "jpeg" {
		params["quality"] = 80
	}

	resp, err := p.sendCDP("Page.captureScreenshot", params, sid)
	if err != nil {
		p.writeErr(w, 500, "screenshot: "+err.Error())
		return
	}

	filePath := r.URL.Query().Get("file")
	var result struct {
		Data string `json:"data"`
	}
	json.Unmarshal(resp.Result, &result)

	if filePath != "" {
		// Save to file
		data, err := base64.StdEncoding.DecodeString(result.Data)
		if err != nil {
			p.writeErr(w, 500, "base64 decode: "+err.Error())
			return
		}
		os.WriteFile(filePath, data, 0644)
		p.writeJSON(w, map[string]interface{}{"saved": filePath})
		return
	}

	// Return as JSON with base64 (for compatibility with existing Manager)
	p.writeJSON(w, map[string]interface{}{"data": result.Data, "format": format})
}

func (p *CDPProxy) handleSetUA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserAgent string `json:"user_agent"`
	}
	body := p.readBody(r)
	if body != "" {
		json.Unmarshal([]byte(body), &req)
	}
	if req.UserAgent == "" {
		p.writeErr(w, 400, "user_agent is required")
		return
	}

	p.mu.Lock()
	p.userAgent = req.UserAgent
	p.mu.Unlock()

	// Apply to all existing sessions
	p.sessionsMu.RLock()
	sessions := make(map[string]string)
	for k, v := range p.sessions {
		sessions[k] = v
	}
	p.sessionsMu.RUnlock()

	for _, sid := range sessions {
		p.sendCDP("Emulation.setUserAgentOverride", map[string]interface{}{
			"userAgent": req.UserAgent,
		}, sid)
	}

	p.writeJSON(w, map[string]interface{}{
		"user_agent":    req.UserAgent,
		"applied_pages": len(sessions),
	})
}

// trackActivity wraps an HTTP handler to update lastActivity timestamp.
func (p *CDPProxy) trackActivity(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.lastActivity.Store(time.Now().Unix())
		fn(w, r)
	}
}

// --- Shared Helper Functions ---

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// isPortOpen checks if a TCP port is listening (SYN-only, no WS, no approval dialog).
func isPortOpen(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// extractPortFromURL extracts the TCP port number from a URL string.
func extractPortFromURL(urlStr string) int {
	parts := strings.Split(urlStr, ":")
	if len(parts) >= 3 {
		portStr := strings.Split(parts[len(parts)-1], "/")[0]
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		return port
	}
	return 0
}

// wsURLCachePath returns the path for caching the WebSocket URL.
func wsURLCachePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".go-claw")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "browser-ws-cache.json")
}

type wsURLCache struct {
	WSURL     string `json:"ws_url"`
	Timestamp int64  `json:"timestamp"`
}

func saveWSURLCache(wsURL string) {
	cache := wsURLCache{WSURL: wsURL, Timestamp: time.Now().Unix()}
	data, _ := json.Marshal(cache)
	os.WriteFile(wsURLCachePath(), data, 0644)
}

func loadWSURLCache() string {
	data, err := os.ReadFile(wsURLCachePath())
	if err != nil {
		return ""
	}
	var cache wsURLCache
	if json.Unmarshal(data, &cache) != nil {
		return ""
	}
	if time.Now().Unix()-cache.Timestamp > 86400 {
		return ""
	}
	return cache.WSURL
}

// readDevToolsActivePort reads Chrome's DevToolsActivePort file.
func readDevToolsActivePort() (string, error) {
	paths := devToolsActivePortPaths()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) < 2 {
			continue
		}
		port := strings.TrimSpace(lines[0])
		wsPath := strings.TrimSpace(lines[1])
		if port == "" || wsPath == "" {
			continue
		}
		return fmt.Sprintf("ws://127.0.0.1:%s%s", port, wsPath), nil
	}
	return "", fmt.Errorf("DevToolsActivePort not found")
}

func devToolsActivePortPaths() []string {
	var paths []string
	if customDir := os.Getenv("CHROME_USER_DATA_DIR"); customDir != "" {
		paths = append(paths, filepath.Join(customDir, "DevToolsActivePort"))
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		paths = append(paths,
			filepath.Join(localAppData, "Google", "Chrome", "User Data", "DevToolsActivePort"),
			filepath.Join(localAppData, "Google", "Chrome Dev", "User Data", "DevToolsActivePort"),
			filepath.Join(localAppData, "Chromium", "User Data", "DevToolsActivePort"),
		)
	case "darwin":
		paths = append(paths,
			filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "DevToolsActivePort"),
			filepath.Join(home, "Library", "Application Support", "Chromium", "DevToolsActivePort"),
		)
	default:
		paths = append(paths,
			filepath.Join(home, ".config", "google-chrome", "DevToolsActivePort"),
			filepath.Join(home, ".config", "chromium", "DevToolsActivePort"),
			filepath.Join(home, ".config", "google-chrome-beta", "DevToolsActivePort"),
		)
	}
	return paths
}

// setDetachAttr sets process attributes for detached child process.
func setDetachAttr(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
		}
	}
	// Unix: no SysProcAttr needed -- child inherits process group by default
	// when parent exits, child continues as orphan
}

// findChrome locates the Chrome executable on the system.
func findChrome() string {
	// Check common locations
	candidates := []string{}
	home, _ := os.UserHomeDir()
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")

	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates,
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
		)
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
		)
	default:
		candidates = append(candidates,
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
		)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fallback: search PATH
	if p, err := exec.LookPath("google-chrome"); err == nil {
		return p
	}
	if p, err := exec.LookPath("chrome"); err == nil {
		return p
	}
	return ""
}

// launchChromeWithDebugPort starts Chrome with remote debugging enabled.
// Uses the user's default Chrome profile so login sessions are preserved.
func launchChromeWithDebugPort(chromePath string, port int) error {
	cmd := exec.Command(chromePath,
		"--remote-debugging-port="+fmt.Sprintf("%d", port),
		"--no-first-run",
		"--no-default-browser-check",
	)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	setDetachAttr(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	cmd.Process.Release()
	fmt.Fprintf(os.Stderr, "  [cdp-proxy] launched Chrome (pid %d) with --remote-debugging-port=%d\n", cmd.Process.Pid, port)
	return nil
}

// browserDataDir returns the Chrome user data directory for managed instances.
func browserDataDir() string {
	if dir := os.Getenv("CLAW_BROWSER_DATA_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".go-claw", "browser-data")
}
