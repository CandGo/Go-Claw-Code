package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteContextFromEnvMap(t *testing.T) {
	env := []string{
		"CLAW_CODE_REMOTE=true",
		"CLAW_CODE_REMOTE_SESSION_ID=session-123",
		"ANTHROPIC_BASE_URL=https://remote.test",
	}
	ctx := RemoteSessionContextFromMap(env)
	if !ctx.Enabled {
		t.Error("should be enabled")
	}
	if ctx.SessionID == nil || *ctx.SessionID != "session-123" {
		t.Error("session_id should be session-123")
	}
	if ctx.BaseURL != "https://remote.test" {
		t.Errorf("base_url = %q, want https://remote.test", ctx.BaseURL)
	}
}

func TestBootstrapFailsOpenWhenMissing(t *testing.T) {
	env := []string{
		"CLAW_CODE_REMOTE=1",
		"CCR_UPSTREAM_PROXY_ENABLED=true",
	}
	bs := UpstreamProxyBootstrapFromMap(env)
	if bs.ShouldEnable() {
		t.Error("should not enable without token and session")
	}
}

func TestBootstrapWithToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "session_token")
	os.WriteFile(tokenPath, []byte("secret-token\n"), 0644)
	caPath := filepath.Join(tmpDir, "ca-bundle.crt")

	env := []string{
		"CLAW_CODE_REMOTE=1",
		"CCR_UPSTREAM_PROXY_ENABLED=true",
		"CLAW_CODE_REMOTE_SESSION_ID=session-123",
		"ANTHROPIC_BASE_URL=https://remote.test",
		"CCR_SESSION_TOKEN_PATH=" + tokenPath,
		"CCR_CA_BUNDLE_PATH=" + caPath,
	}

	bs := UpstreamProxyBootstrapFromMap(env)
	if !bs.ShouldEnable() {
		t.Fatal("should be enabled")
	}
	if bs.Token == nil || *bs.Token != "secret-token" {
		t.Errorf("token = %v, want secret-token", bs.Token)
	}
	if bs.WSURL() != "wss://remote.test/v1/code/upstreamproxy/ws" {
		t.Errorf("ws_url = %q", bs.WSURL())
	}

	state := bs.StateForPort(9443)
	if !state.Enabled {
		t.Fatal("state should be enabled")
	}
	subEnv := state.SubprocessEnv()
	if subEnv["HTTPS_PROXY"] != "http://127.0.0.1:9443" {
		t.Errorf("HTTPS_PROXY = %q", subEnv["HTTPS_PROXY"])
	}
	if subEnv["SSL_CERT_FILE"] != caPath {
		t.Errorf("SSL_CERT_FILE = %q", subEnv["SSL_CERT_FILE"])
	}
}

func TestReadToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "session_token")
	os.WriteFile(tokenPath, []byte(" abc123 \n"), 0644)

	token := readToken(tokenPath)
	if token == nil || *token != "abc123" {
		t.Errorf("token = %v, want abc123", token)
	}

	missing := readToken(filepath.Join(tmpDir, "missing"))
	if missing != nil {
		t.Error("missing file should return nil")
	}
}

func TestInheritedUpstreamProxyEnv(t *testing.T) {
	envMap := map[string]string{
		"HTTPS_PROXY":   "http://127.0.0.1:8888",
		"SSL_CERT_FILE": "/tmp/ca-bundle.crt",
		"NO_PROXY":      "localhost",
	}
	inherited := InheritedUpstreamProxyEnv(envMap)
	if len(inherited) != 3 {
		t.Errorf("expected 3 keys, got %d", len(inherited))
	}
	if inherited["NO_PROXY"] != "localhost" {
		t.Errorf("NO_PROXY = %q", inherited["NO_PROXY"])
	}
	if len(InheritedUpstreamProxyEnv(map[string]string{})) != 0 {
		t.Error("empty map should return empty")
	}
}

func TestUpstreamProxyWSURL(t *testing.T) {
	if got := UpstreamProxyWSURL("http://localhost:3000/"); got != "ws://localhost:3000/v1/code/upstreamproxy/ws" {
		t.Errorf("ws_url = %q", got)
	}
	if got := UpstreamProxyWSURL("https://api.anthropic.com"); got != "wss://api.anthropic.com/v1/code/upstreamproxy/ws" {
		t.Errorf("ws_url = %q", got)
	}
}

func TestNoProxyList(t *testing.T) {
	list := noProxyList()
	if !strings.Contains(list, "anthropic.com") {
		t.Error("should contain anthropic.com")
	}
	if !strings.Contains(list, "github.com") {
		t.Error("should contain github.com")
	}
}
