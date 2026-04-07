package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRemoteBaseURL    = "https://api.anthropic.com"
	DefaultSessionTokenPath = "/run/ccr/session_token"
	DefaultSystemCABundle   = "/etc/ssl/certs/ca-certificates.crt"
)

// UpstreamProxyEnvKeys mirrors Rust UPSTREAM_PROXY_ENV_KEYS.
var UpstreamProxyEnvKeys = []string{
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
	"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS",
	"REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE",
}

// NoProxyHosts mirrors Rust NO_PROXY_HOSTS.
var NoProxyHosts = []string{
	"localhost", "127.0.0.1", "::1",
	"169.254.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"anthropic.com", ".anthropic.com", "*.anthropic.com",
	"github.com", "api.github.com", "*.github.com", "*.githubusercontent.com",
	"registry.npmjs.org", "index.crates.io",
}

// RemoteSessionContext mirrors Rust RemoteSessionContext.
type RemoteSessionContext struct {
	Enabled   bool
	SessionID *string
	BaseURL   string
	EnvType   string // "local", "ssh", "wsl", "docker", "remote"
}

// IsRemote returns true if this is a remote session (backward-compatible).
func (c RemoteSessionContext) IsRemote() bool {
	return c.Enabled
}

// SessionType returns a string describing the session type (backward-compatible).
func (c RemoteSessionContext) SessionType() string {
	if c.EnvType != "" {
		return c.EnvType
	}
	if c.Enabled {
		return "remote"
	}
	return "local"
}

// UpstreamProxyBootstrap mirrors Rust UpstreamProxyBootstrap.
type UpstreamProxyBootstrap struct {
	Remote               RemoteSessionContext
	UpstreamProxyEnabled bool
	TokenPath            string
	CABundlePath         string
	SystemCAPath         string
	Token                *string
}

// UpstreamProxyState mirrors Rust UpstreamProxyState.
type UpstreamProxyState struct {
	Enabled      bool
	ProxyURL     *string
	CABundlePath *string
	NoProxy      string
}

// NewRemoteSessionContext mirrors Rust RemoteSessionContext::from_env.
func NewRemoteSessionContext() RemoteSessionContext {
	return RemoteSessionContextFromMap(os.Environ())
}

// RemoteSessionContextFromMap mirrors Rust RemoteSessionContext::from_env_map.
func RemoteSessionContextFromMap(envPairs []string) RemoteSessionContext {
	envMap := envPairsToMap(envPairs)
	return RemoteSessionContext{
		Enabled:   envTruthy(envMap["CLAW_CODE_REMOTE"]),
		SessionID: nonEmptyStr(envMap["CLAW_CODE_REMOTE_SESSION_ID"]),
		BaseURL:   orDefault(nonEmptyStr(envMap["ANTHROPIC_BASE_URL"]), DefaultRemoteBaseURL),
	}
}

// NewUpstreamProxyBootstrap mirrors Rust UpstreamProxyBootstrap::from_env.
func NewUpstreamProxyBootstrap() UpstreamProxyBootstrap {
	return UpstreamProxyBootstrapFromMap(os.Environ())
}

// UpstreamProxyBootstrapFromMap mirrors Rust UpstreamProxyBootstrap::from_env_map.
func UpstreamProxyBootstrapFromMap(envPairs []string) UpstreamProxyBootstrap {
	envMap := envPairsToMap(envPairs)
	remote := RemoteSessionContextFromMap(envPairs)

	tokenPath := orDefault(nonEmptyStr(envMap["CCR_SESSION_TOKEN_PATH"]), DefaultSessionTokenPath)
	systemCAPath := orDefault(nonEmptyStr(envMap["CCR_SYSTEM_CA_BUNDLE"]), DefaultSystemCABundle)
	caBundlePath := orDefault(nonEmptyStr(envMap["CCR_CA_BUNDLE_PATH"]), defaultCABundlePath())

	token := readToken(tokenPath)

	return UpstreamProxyBootstrap{
		Remote:               remote,
		UpstreamProxyEnabled: envTruthy(envMap["CCR_UPSTREAM_PROXY_ENABLED"]),
		TokenPath:            tokenPath,
		CABundlePath:         caBundlePath,
		SystemCAPath:         systemCAPath,
		Token:                token,
	}
}

// ShouldEnable mirrors Rust UpstreamProxyBootstrap::should_enable.
func (b *UpstreamProxyBootstrap) ShouldEnable() bool {
	return b.Remote.Enabled && b.UpstreamProxyEnabled &&
		b.Remote.SessionID != nil && b.Token != nil
}

// WSURL mirrors Rust UpstreamProxyBootstrap::ws_url.
func (b *UpstreamProxyBootstrap) WSURL() string {
	return UpstreamProxyWSURL(b.Remote.BaseURL)
}

// StateForPort mirrors Rust UpstreamProxyBootstrap::state_for_port.
func (b *UpstreamProxyBootstrap) StateForPort(port uint16) UpstreamProxyState {
	if !b.ShouldEnable() {
		return DisabledUpstreamProxyState()
	}
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	caPath := b.CABundlePath
	return UpstreamProxyState{
		Enabled:      true,
		ProxyURL:     &proxyURL,
		CABundlePath: &caPath,
		NoProxy:      noProxyList(),
	}
}

// DisabledUpstreamProxyState mirrors Rust UpstreamProxyState::disabled.
func DisabledUpstreamProxyState() UpstreamProxyState {
	return UpstreamProxyState{
		Enabled: false,
		NoProxy: noProxyList(),
	}
}

// SubprocessEnv mirrors Rust UpstreamProxyState::subprocess_env.
func (s *UpstreamProxyState) SubprocessEnv() map[string]string {
	if !s.Enabled || s.ProxyURL == nil || s.CABundlePath == nil {
		return map[string]string{}
	}
	caPath := *s.CABundlePath
	return map[string]string{
		"HTTPS_PROXY":         *s.ProxyURL,
		"https_proxy":         *s.ProxyURL,
		"NO_PROXY":            s.NoProxy,
		"no_proxy":            s.NoProxy,
		"SSL_CERT_FILE":       caPath,
		"NODE_EXTRA_CA_CERTS": caPath,
		"REQUESTS_CA_BUNDLE":  caPath,
		"CURL_CA_BUNDLE":      caPath,
	}
}

// ReadToken mirrors Rust read_token.
func ReadToken(path string) (*string, error) {
	return readToken(path), nil
}

func readToken(path string) *string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return nil
	}
	return &token
}

// UpstreamProxyWSURL mirrors Rust upstream_proxy_ws_url.
func UpstreamProxyWSURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	var wsBase string
	if strings.HasPrefix(base, "https://") {
		wsBase = "wss://" + strings.TrimPrefix(base, "https://")
	} else if strings.HasPrefix(base, "http://") {
		wsBase = "ws://" + strings.TrimPrefix(base, "http://")
	} else {
		wsBase = "wss://" + base
	}
	return wsBase + "/v1/code/upstreamproxy/ws"
}

// NoProxyList mirrors Rust no_proxy_list.
func noProxyList() string {
	hosts := make([]string, len(NoProxyHosts))
	copy(hosts, NoProxyHosts)
	hosts = append(hosts, "pypi.org", "files.pythonhosted.org", "proxy.golang.org")
	return strings.Join(hosts, ",")
}

// InheritedUpstreamProxyEnv mirrors Rust inherited_upstream_proxy_env.
func InheritedUpstreamProxyEnv(envMap map[string]string) map[string]string {
	if _, ok := envMap["HTTPS_PROXY"]; !ok {
		return map[string]string{}
	}
	if _, ok := envMap["SSL_CERT_FILE"]; !ok {
		return map[string]string{}
	}
	result := map[string]string{}
	for _, key := range UpstreamProxyEnvKeys {
		if val, ok := envMap[key]; ok {
			result[key] = val
		}
	}
	return result
}

// DetectRemoteContext probes the environment for remote/forwarded session indicators.
// Backward-compatible helper.
func DetectRemoteContext() RemoteSessionContext {
	return NewRemoteSessionContext()
}

// DetectEnvironment probes the environment and returns a RemoteSessionContext
// with detected environment type.
func DetectEnvironment() RemoteSessionContext {
	ctx := DetectRemoteContext()

	// Check for SSH session
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
		ctx.EnvType = "ssh"
		return ctx
	}

	// Check for WSL (Windows Subsystem for Linux)
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
			ctx.EnvType = "wsl"
			return ctx
		}
	}

	// Check for Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		ctx.EnvType = "docker"
		return ctx
	}

	if ctx.Enabled {
		ctx.EnvType = "remote"
	} else {
		ctx.EnvType = "local"
	}
	return ctx
}

// ProxyEnv returns proxy-related environment variables.
// Backward-compatible helper.
func ProxyEnv() map[string]string {
	envs := map[string]string{}
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
	} {
		if v := os.Getenv(key); v != "" {
			envs[key] = v
		}
	}
	return envs
}

func defaultCABundlePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".ccr", "ca-bundle.crt")
}

func envTruthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func envPairsToMap(pairs []string) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func nonEmptyStr(val string) *string {
	if val == "" {
		return nil
	}
	return &val
}

func orDefault(val *string, def string) string {
	if val != nil {
		return *val
	}
	return def
}
