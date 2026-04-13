package browser

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Manager manages browser operations via the CDP proxy.
// It supports multiple tabs — each tool call specifies a target tab ID.
type Manager struct {
	proxyURL   string
	httpClient *http.Client

	// Proxy readiness
	proxyReady atomic.Bool
}

// DefaultManager is the singleton browser manager.
var DefaultManager *Manager

// ConnectResult holds info about a browser connection.
type ConnectResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Success bool   `json:"success"`
}

// NewManager creates a new browser manager.
func NewManager() *Manager {
	port := ProxyPort()
	return &Manager{
		proxyURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// proxyPIDFile returns the path to the proxy PID file.
func proxyPIDFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".go-claw", "cdp-proxy.pid")
}

// saveProxyPID saves the proxy process ID.
func saveProxyPID(pid int) {
	os.WriteFile(proxyPIDFile(), []byte(fmt.Sprintf("%d", pid)), 0644)
}

// EnsureProxy ensures the CDP proxy is running.
func (m *Manager) EnsureProxy() error {
	if m.proxyReady.Load() && IsProxyRunning(ProxyPort()) {
		return nil
	}

	port := ProxyPort()
	if IsProxyRunning(port) {
		m.proxyReady.Store(true)
		return nil
	}

	// Start proxy as a detached background process
	if err := startDetachedProxy(); err != nil {
		fmt.Fprintf(os.Stderr, "  [browser] failed to start detached proxy: %v, trying in-process\n", err)
		proxy := NewCDPProxy(port)
		defaultProxy = proxy
		if err := proxy.Start(); err != nil {
			return fmt.Errorf("failed to start CDP proxy: %w", err)
		}
	}

	// Wait for proxy HTTP server to be ready
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if IsProxyRunning(port) {
			m.proxyReady.Store(true)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !m.proxyReady.Load() {
		return fmt.Errorf("CDP proxy failed to start on port %d", port)
	}
	fmt.Fprintf(os.Stderr, "  [browser] CDP proxy ready\n")
	return nil
}

// startDetachedProxy launches the CDP proxy as a detached background process.
func startDetachedProxy() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--cdp-proxy")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	setDetachAttr(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	saveProxyPID(cmd.Process.Pid)
	fmt.Fprintf(os.Stderr, "  [browser] started detached CDP proxy (pid %d)\n", cmd.Process.Pid)
	cmd.Process.Release()
	return nil
}

// --- Tab Management ---

// NewTab creates a new browser tab and returns its target ID.
func (m *Manager) NewTab(url string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	targetURL := "about:blank"
	if url != "" {
		targetURL = url
	}
	resp, err := m.get(fmt.Sprintf("/new?url=%s", urlEncode(targetURL)))
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	targetID, _ := resp["targetId"].(string)
	if targetID == "" {
		return "", fmt.Errorf("no targetId in response")
	}
	return targetID, nil
}

// ListTabs returns all open browser tabs.
func (m *Manager) ListTabs() ([]map[string]interface{}, error) {
	if err := m.EnsureProxy(); err != nil {
		return nil, err
	}
	resp, err := m.get("/targets")
	if err != nil {
		return nil, err
	}
	if errMsg := getError(resp); errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	// Response is a JSON array
	if arr, ok := resp["__array"]; ok {
		return arr.([]map[string]interface{}), nil
	}
	return nil, nil
}

// CloseTab closes a browser tab.
func (m *Manager) CloseTab(targetID string) error {
	if targetID == "" {
		return fmt.Errorf("targetID is required")
	}
	resp, err := m.get(fmt.Sprintf("/close?target=%s", targetID))
	if err != nil {
		return err
	}
	if errMsg := getError(resp); errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

// --- Page Operations (all take targetID) ---

// Navigate navigates a tab to a URL.
func (m *Manager) Navigate(targetID, pageURL string) (*ConnectResult, error) {
	if err := m.EnsureProxy(); err != nil {
		return nil, err
	}
	resp, err := m.get(fmt.Sprintf("/navigate?target=%s&url=%s", targetID, urlEncode(pageURL)))
	if err != nil {
		return nil, err
	}
	if errMsg := getError(resp); errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	result := &ConnectResult{Success: true}
	// Get page info for title/URL
	info, _ := m.GetInfo(targetID)
	if info != nil {
		result.URL = info["url"].(string)
		result.Title = info["title"].(string)
	}
	return result, nil
}

// Click clicks on an element (JS-level).
func (m *Manager) Click(targetID, selector string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	resp, err := m.postBody(fmt.Sprintf("/click?target=%s", targetID), selector)
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return fmt.Sprintf("Clicked %s", selector), nil
}

// ClickAt performs a real CDP mouse click.
func (m *Manager) ClickAt(targetID, selector string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	resp, err := m.postBody(fmt.Sprintf("/clickAt?target=%s", targetID), selector)
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return fmt.Sprintf("Real-clicked %s", selector), nil
}

// Type types text into an element.
func (m *Manager) Type(targetID, selector, text string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	js := fmt.Sprintf(`(() => {
		var el = document.querySelector(%s);
		if (!el) return 'element not found';
		el.focus();
		var proto = Object.getPrototypeOf(el);
		var desc = Object.getOwnPropertyDescriptor(proto, 'value');
		if (desc && desc.set) { desc.set.call(el, %s); } else { el.value = %s; }
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
		return 'ok';
	})()`, jsonEncode(selector), jsonEncode(text), jsonEncode(text))
	resp, err := m.postBody(fmt.Sprintf("/eval?target=%s", targetID), js)
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return fmt.Sprintf("Typed into %s: %s", selector, text), nil
}

// PressKey presses a keyboard key.
func (m *Manager) PressKey(targetID, key string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	js := fmt.Sprintf(`(() => { document.dispatchEvent(new KeyboardEvent('keydown', {key: %s})); return 'ok'; })()`, jsonEncode(key))
	resp, err := m.postBody(fmt.Sprintf("/eval?target=%s", targetID), js)
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return fmt.Sprintf("Pressed key: %s", key), nil
}

// Scroll scrolls the page.
func (m *Manager) Scroll(targetID, direction string, y int) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	resp, err := m.get(fmt.Sprintf("/scroll?target=%s&direction=%s&y=%d", targetID, direction, y))
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return fmt.Sprintf("Scrolled %s", direction), nil
}

// Back goes back in browser history.
func (m *Manager) Back(targetID string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	resp, err := m.get(fmt.Sprintf("/back?target=%s", targetID))
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	return "Went back", nil
}

// Eval executes JavaScript in the page.
func (m *Manager) Eval(targetID, js string) (string, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", err
	}
	resp, err := m.postBody(fmt.Sprintf("/eval?target=%s", targetID), js)
	if err != nil {
		return "", err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	if val, ok := resp["value"]; ok {
		return fmt.Sprintf("%v", val), nil
	}
	return "ok", nil
}

// SetFiles sets files on a file input element.
func (m *Manager) SetFiles(targetID, selector string, files []string) error {
	if err := m.EnsureProxy(); err != nil {
		return err
	}
	body := map[string]interface{}{"selector": selector, "files": files}
	data, _ := json.Marshal(body)
	resp, err := m.postJSON(fmt.Sprintf("/setFiles?target=%s", targetID), data)
	if err != nil {
		return err
	}
	if errMsg := getError(resp); errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

// GetTextContent returns visible text from the page.
func (m *Manager) GetTextContent(targetID string) (string, error) {
	return m.Eval(targetID, `document.body.innerText`)
}

// Screenshot takes a screenshot of the page.
func (m *Manager) Screenshot(targetID string) (string, int, int, error) {
	if err := m.EnsureProxy(); err != nil {
		return "", 0, 0, err
	}
	resp, err := m.get(fmt.Sprintf("/screenshot?target=%s", targetID))
	if err != nil {
		return "", 0, 0, err
	}
	if errMsg := getError(resp); errMsg != "" {
		return "", 0, 0, fmt.Errorf("%s", errMsg)
	}
	b64, _ := resp["data"].(string)
	// Get viewport dimensions via info
	info, _ := m.GetInfo(targetID)
	w, h := 0, 0
	if info != nil {
		if v, ok := info["w"].(float64); ok {
			w = int(v)
		}
		if v, ok := info["h"].(float64); ok {
			h = int(v)
		}
	}
	return b64, w, h, nil
}

// QuerySelectorAll returns text content of all matching elements.
func (m *Manager) QuerySelectorAll(targetID, selector string) (string, error) {
	js := fmt.Sprintf(`(() => {
		var els = document.querySelectorAll(%s);
		var texts = [];
		for (var i = 0; i < Math.min(els.length, 200); i++) {
			texts.push('[' + i + '] ' + (els[i].textContent || '').trim().slice(0, 500));
		}
		return JSON.stringify(texts);
	})()`, jsonEncode(selector))
	result, err := m.Eval(targetID, js)
	if err != nil {
		return "", err
	}
	var texts []string
	if err := json.Unmarshal([]byte(strings.Trim(result, `"`)), &texts); err == nil {
		return strings.Join(texts, "\n"), nil
	}
	return result, nil
}

// GetInfo returns page info (url, title, ready, w, h).
func (m *Manager) GetInfo(targetID string) (map[string]interface{}, error) {
	js := `JSON.stringify({title: document.title, url: location.href, ready: document.readyState, w: window.innerWidth, h: window.innerHeight})`
	result, err := m.Eval(targetID, js)
	if err != nil {
		return nil, err
	}
	var info map[string]interface{}
	cleaned := strings.Trim(result, `"`)
	cleaned = strings.ReplaceAll(cleaned, `\"`, `"`)
	if err := json.Unmarshal([]byte(cleaned), &info); err != nil {
		return nil, err
	}
	return info, nil
}

// GetPageURL returns the current page URL and title.
func (m *Manager) GetPageURL(targetID string) (string, string, error) {
	info, err := m.GetInfo(targetID)
	if err != nil {
		return "", "", err
	}
	u, _ := info["url"].(string)
	t, _ := info["title"].(string)
	return u, t, nil
}

// Close closes the browser manager.
func (m *Manager) Close() {
	// Don't stop the proxy — it persists across sessions
}

// Health returns proxy health status.
func (m *Manager) Health() (map[string]interface{}, error) {
	return m.get("/health")
}

// --- HTTP helpers ---

func (m *Manager) get(path string) (map[string]interface{}, error) {
	resp, err := m.httpClient.Get(m.proxyURL + path)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Try JSON first
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err == nil {
		return result, nil
	}

	// Try JSON array (targets returns an array)
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err == nil {
		return map[string]interface{}{"__array": arr}, nil
	}

	return map[string]interface{}{"text": string(body)}, nil
}

func (m *Manager) postBody(path string, body string) (map[string]interface{}, error) {
	resp, err := m.httpClient.Post(m.proxyURL+path, "text/plain", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	data, _ := io.ReadAll(resp.Body)
	json.Unmarshal(data, &result)
	return result, nil
}

func (m *Manager) postJSON(path string, body []byte) (map[string]interface{}, error) {
	resp, err := m.httpClient.Post(m.proxyURL+path, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	data, _ := io.ReadAll(resp.Body)
	json.Unmarshal(data, &result)
	return result, nil
}

func getError(resp map[string]interface{}) string {
	if errMsg, ok := resp["error"].(string); ok {
		return errMsg
	}
	return ""
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

func jsonEncode(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// RunCDPProxy starts a standalone CDP proxy server (for --cdp-proxy flag).
func RunCDPProxy() error {
	port := ProxyPort()
	if IsProxyRunning(port) {
		fmt.Printf("CDP Proxy already running on port %d\n", port)
		return nil
	}

	proxy := NewCDPProxy(port)
	if err := proxy.Start(); err != nil {
		return err
	}
	saveProxyPID(os.Getpid())
	fmt.Printf("CDP Proxy running on port %d (pid %d). Press Ctrl+C to stop.\n", port, os.Getpid())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nCDP Proxy shutting down...")
	proxy.Stop()
	return nil
}

// --- Site Experience ---

// SiteExperienceDir returns the directory for site experience files.
func SiteExperienceDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".go-claw", "site-patterns")
}

// SiteExperiencePath returns the path for a specific domain's experience file.
func SiteExperiencePath(domain string) string {
	return filepath.Join(SiteExperienceDir(), domain+".md")
}

// ListSiteExperiences lists all stored site experience domains.
func ListSiteExperiences() []string {
	dir := SiteExperienceDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var domains []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			domains = append(domains, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return domains
}

// ReadSiteExperience reads the experience file for a domain.
func ReadSiteExperience(domain string) (string, error) {
	data, err := os.ReadFile(SiteExperiencePath(domain))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteSiteExperience writes an experience file for a domain.
func WriteSiteExperience(domain, content string) error {
	dir := SiteExperienceDir()
	os.MkdirAll(dir, 0755)
	return os.WriteFile(SiteExperiencePath(domain), []byte(content), 0644)
}
