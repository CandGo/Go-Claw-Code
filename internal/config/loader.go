package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CLAWSettingsSchemaName mirrors Rust CLAW_SETTINGS_SCHEMA_NAME.
const CLAWSettingsSchemaName = "SettingsSchema"

// --- ConfigSource ---

// ConfigSource identifies where a config value came from.
type ConfigSource int

const (
	SourceUser ConfigSource = iota
	SourceProject
	SourceLocal
)

func (s ConfigSource) String() string {
	switch s {
	case SourceUser:
		return "User"
	case SourceProject:
		return "Project"
	case SourceLocal:
		return "Local"
	default:
		return "Unknown"
	}
}

// --- ResolvedPermissionMode ---

// ResolvedPermissionMode is the resolved permission mode.
type ResolvedPermissionMode int

const (
	PermReadOnly ResolvedPermissionMode = iota
	PermWorkspaceWrite
	PermDangerFullAccess
)

func (m ResolvedPermissionMode) String() string {
	switch m {
	case PermReadOnly:
		return "read-only"
	case PermWorkspaceWrite:
		return "workspace-write"
	case PermDangerFullAccess:
		return "danger-full-access"
	default:
		return "unknown"
	}
}

// --- ConfigEntry ---

// ConfigEntry represents a loaded config file with its source and path.
type ConfigEntry struct {
	Source ConfigSource
	Path   string
}

// ConfigValueEntry tracks an individual config key-value pair and its source.
type ConfigValueEntry struct {
	Key    string // e.g. "model", "permissionMode", "hooks.PreToolUse[0]"
	Value  string
	Source string // e.g. "user", "project", "local", "env"
}

// --- ConfigError ---

// ConfigError represents an error during config loading.
type ConfigError struct {
	Kind    string // "io" or "parse"
	Message string
	Cause   error
}

func (e *ConfigError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ConfigError) Unwrap() error {
	return e.Cause
}

// --- McpTransport ---

// McpTransport enumerates MCP server transport types.
type McpTransport int

const (
	McpTransportStdio McpTransport = iota
	McpTransportSse
	McpTransportHttp
	McpTransportWs
	McpTransportSdk
	McpTransportManagedProxy
)

func (t McpTransport) String() string {
	switch t {
	case McpTransportStdio:
		return "stdio"
	case McpTransportSse:
		return "sse"
	case McpTransportHttp:
		return "http"
	case McpTransportWs:
		return "ws"
	case McpTransportSdk:
		return "sdk"
	case McpTransportManagedProxy:
		return "managed-proxy"
	default:
		return "unknown"
	}
}

// --- MCP server config types (mirrors Rust McpServerConfig variants) ---

// McpStdioServerConfig holds stdio MCP server configuration.
type McpStdioServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

// McpRemoteServerConfig holds SSE/HTTP MCP server configuration.
type McpRemoteServerConfig struct {
	URL           string
	Headers       map[string]string
	HeadersHelper string
	OAuth         *McpOAuthConfig
}

// McpWebSocketServerConfig holds WebSocket MCP server configuration.
type McpWebSocketServerConfig struct {
	URL           string
	Headers       map[string]string
	HeadersHelper string
}

// McpSdkServerConfig holds SDK MCP server configuration.
type McpSdkServerConfig struct {
	Name string
}

// McpManagedProxyServerConfig holds managed proxy MCP server configuration.
type McpManagedProxyServerConfig struct {
	URL string
	ID  string
}

// McpOAuthConfig holds MCP server OAuth configuration.
type McpOAuthConfig struct {
	ClientID               string
	CallbackPort           *uint16
	AuthServerMetadataURL  string
	Xaa                    *bool
}

// McpServerConfig is a tagged union of MCP server configuration variants.
type McpServerConfig struct {
	Transport    McpTransport
	Stdio        *McpStdioServerConfig
	Remote       *McpRemoteServerConfig
	WebSocket    *McpWebSocketServerConfig
	SDK          *McpSdkServerConfig
	ManagedProxy *McpManagedProxyServerConfig
}

// ScopedMcpServerConfig holds an MCP server config with its scope.
type ScopedMcpServerConfig struct {
	Scope  ConfigSource
	Config McpServerConfig
}

// McpConfigCollection holds all discovered MCP server configurations.
type McpConfigCollection struct {
	Servers map[string]ScopedMcpServerConfig
}

// Get returns a scoped MCP server config by name.
func (c *McpConfigCollection) Get(name string) (ScopedMcpServerConfig, bool) {
	s, ok := c.Servers[name]
	return s, ok
}

// --- OAuthConfig ---

// OAuthConfig holds runtime OAuth configuration.
type OAuthConfig struct {
	ClientID           string
	AuthorizeURL       string
	TokenURL           string
	CallbackPort       *uint16
	ManualRedirectURL  string
	Scopes             []string
}

// --- RuntimeHookConfig ---

// RuntimeHookConfig holds hook commands for all hook types.
type RuntimeHookConfig struct {
	PreToolUse      []string
	PostToolUse     []string
	Notification    []string
	Stop            []string
	SubagentBefore  []string
	SubagentAfter   []string
}

// NewRuntimeHookConfig creates a RuntimeHookConfig with the given hook lists.
func NewRuntimeHookConfig(pre, post []string) RuntimeHookConfig {
	return RuntimeHookConfig{PreToolUse: pre, PostToolUse: post}
}

// PreToolUseHooks returns the pre-tool-use hooks.
func (c *RuntimeHookConfig) PreToolUseHooks() []string {
	return c.PreToolUse
}

// PostToolUseHooks returns the post-tool-use hooks.
func (c *RuntimeHookConfig) PostToolUseHooks() []string {
	return c.PostToolUse
}

// Merged returns a new RuntimeHookConfig with hooks from both configs merged.
func (c *RuntimeHookConfig) Merged(other *RuntimeHookConfig) RuntimeHookConfig {
	result := *c
	result.Extend(other)
	return result
}

// Extend appends hooks from other, deduplicating.
func (c *RuntimeHookConfig) Extend(other *RuntimeHookConfig) {
	extendUnique(&c.PreToolUse, other.PreToolUse)
	extendUnique(&c.PostToolUse, other.PostToolUse)
	extendUnique(&c.Notification, other.Notification)
	extendUnique(&c.Stop, other.Stop)
	extendUnique(&c.SubagentBefore, other.SubagentBefore)
	extendUnique(&c.SubagentAfter, other.SubagentAfter)
}

// --- RuntimePluginConfig ---

// RuntimePluginConfig holds plugin settings (mirrors Rust RuntimePluginConfig).
type RuntimePluginConfig struct {
	EnabledPlugins     map[string]bool
	ExternalDirectories []string
	InstallRoot        string
	RegistryPath       string
	BundledRoot        string
}

// StateFor returns the enabled state for a plugin, using defaultEnabled if not set.
func (c *RuntimePluginConfig) StateFor(pluginID string, defaultEnabled bool) bool {
	if v, ok := c.EnabledPlugins[pluginID]; ok {
		return v
	}
	return defaultEnabled
}

// SetPluginState sets the enabled state for a plugin.
func (c *RuntimePluginConfig) SetPluginState(pluginID string, enabled bool) {
	if c.EnabledPlugins == nil {
		c.EnabledPlugins = make(map[string]bool)
	}
	c.EnabledPlugins[pluginID] = enabled
}

// --- RuntimeFeatureConfig ---

// RuntimeFeatureConfig controls runtime feature toggles.
type RuntimeFeatureConfig struct {
	VimMode       bool `json:"vimMode,omitempty"`
	Streaming     bool `json:"streaming,omitempty"`
	ThinkingBlock bool `json:"thinkingBlock,omitempty"`
	SandboxLevel  int  `json:"sandboxLevel,omitempty"` // 0=off, 1=path, 2=env, 3=namespace
}

// --- SandboxConfig (config layer) ---

// SandboxConfig holds sandbox settings in config.
type SandboxConfig struct {
	Enabled              bool     `json:"enabled,omitempty"`
	AllowNet             bool     `json:"allowNet,omitempty"`
	AllowPaths           []string `json:"allowPaths,omitempty"`
	Isolation            int      `json:"isolation,omitempty"` // 1=path, 2=env, 3=namespace
	NamespaceRestrictions *bool   `json:"namespaceRestrictions,omitempty"`
	NetworkIsolation     *bool    `json:"networkIsolation,omitempty"`
	FilesystemMode       string   `json:"filesystemMode,omitempty"` // "off", "workspace-only", "allow-list"
	AllowedMounts        []string `json:"allowedMounts,omitempty"`
}

// --- Backward-compatible types ---

// HookConfig holds pre/post tool hook commands (backward-compatible alias).
type HookConfig = RuntimeHookConfig

// MCPServer describes an MCP server connection (backward-compatible, flattened from variants).
type MCPServer struct {
	Type    string            `json:"type"`              // stdio, sse, http, ws, sdk, managed-proxy
	Command string            `json:"command,omitempty"` // stdio: command to spawn
	Args    []string          `json:"args,omitempty"`    // stdio: command arguments
	URL     string            `json:"url,omitempty"`     // sse/http/ws: server URL
	Headers map[string]string `json:"headers,omitempty"` // sse/http/ws: auth headers
	Env     map[string]string `json:"env,omitempty"`     // stdio: environment variables
	Config  map[string]interface{} `json:"config,omitempty"` // transport-specific config
}

// PluginConfig holds plugin settings (backward-compatible).
type PluginConfig struct {
	Enabled []string `json:"enabled,omitempty"`
}

// --- Config (top-level, mirrors Rust RuntimeConfig) ---

// Config represents the merged application configuration.
type Config struct {
	Model          string                 `json:"model,omitempty"`
	PermissionMode string                 `json:"permissionMode,omitempty"`
	Hooks          RuntimeHookConfig      `json:"hooks,omitempty"`
	MCPServers     map[string]MCPServer   `json:"mcp_servers,omitempty"`
	Plugins        PluginConfig           `json:"plugins,omitempty"`
	Sandbox        SandboxConfig          `json:"sandbox,omitempty"`
	Features       RuntimeFeatureConfig   `json:"features,omitempty"`
	Settings       map[string]interface{} `json:"settings,omitempty"`

	// Internal fields from Rust RuntimeConfig
	loadedEntries []ConfigEntry
	valueEntries  []ConfigValueEntry
	merged        map[string]interface{}
	// Enhanced internal types
	mcpCollection  McpConfigCollection
	pluginConfig   RuntimePluginConfig
	oauth          *OAuthConfig
	resolvedPerm   *ResolvedPermissionMode
}

// LoadedEntries returns the list of config entries that were loaded.
func (c *Config) LoadedEntries() []ConfigEntry {
	return c.loadedEntries
}

// McpCollection returns the typed MCP server collection.
func (c *Config) McpCollection() *McpConfigCollection {
	return &c.mcpCollection
}

// PluginRuntimeConfig returns the typed plugin config.
func (c *Config) PluginRuntimeConfig() *RuntimePluginConfig {
	return &c.pluginConfig
}

// OAuth returns the OAuth config if present.
func (c *Config) OAuth() *OAuthConfig {
	return c.oauth
}

// ResolvedPermissionMode returns the resolved permission mode.
func (c *Config) ResolvedPermMode() *ResolvedPermissionMode {
	return c.resolvedPerm
}

// Validate checks the config for errors and returns them.
func (c *Config) Validate() error {
	var errs []string

	// Validate permission mode
	validModes := map[string]bool{
		"danger-full-access": true,
		"plan":               true,
		"auto":               true,
		"ask":                true,
		"default":            true,
		"read-only":          true,
		"acceptEdits":        true,
		"dontAsk":            true,
		"workspace-write":    true,
	}
	if c.PermissionMode != "" && !validModes[c.PermissionMode] {
		errs = append(errs, fmt.Sprintf("invalid permissionMode: %s (valid: danger-full-access, plan, auto, ask, read-only, workspace-write)", c.PermissionMode))
	}

	// Validate MCP servers
	for name, srv := range c.MCPServers {
		validTypes := map[string]bool{
			"stdio": true, "sse": true, "http": true, "ws": true, "sdk": true, "managed-proxy": true, "claudeai-proxy": true,
		}
		if srv.Type != "" && !validTypes[srv.Type] {
			errs = append(errs, fmt.Sprintf("mcp_server %s: invalid type %s", name, srv.Type))
		}
		if srv.Type == "stdio" && srv.Command == "" {
			errs = append(errs, fmt.Sprintf("mcp_server %s: stdio requires command", name))
		}
		if (srv.Type == "sse" || srv.Type == "http" || srv.Type == "ws") && srv.URL == "" {
			errs = append(errs, fmt.Sprintf("mcp_server %s: %s requires url", name, srv.Type))
		}
	}

	// Validate sandbox isolation level
	if c.Sandbox.Isolation < 0 || c.Sandbox.Isolation > 3 {
		errs = append(errs, "sandbox.isolation must be 0-3")
	}

	// Validate sandbox filesystem mode
	if c.Sandbox.FilesystemMode != "" {
		validModes := map[string]bool{"off": true, "workspace-only": true, "allow-list": true}
		if !validModes[c.Sandbox.FilesystemMode] {
			errs = append(errs, fmt.Sprintf("sandbox.filesystemMode: unsupported filesystem mode %s", c.Sandbox.FilesystemMode))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- ConfigLoader ---

// ConfigLoader discovers and loads configuration files (mirrors Rust ConfigLoader).
type ConfigLoader struct {
	cwd        string
	configHome string
}

// NewConfigLoader creates a ConfigLoader with explicit paths.
func NewConfigLoader(cwd, configHome string) *ConfigLoader {
	return &ConfigLoader{cwd: cwd, configHome: configHome}
}

// DefaultConfigLoader creates a ConfigLoader with default paths.
func DefaultConfigLoader(cwd string) *ConfigLoader {
	return &ConfigLoader{cwd: cwd, configHome: defaultConfigHome()}
}

// ConfigHome returns the config home directory.
func (l *ConfigLoader) ConfigHome() string {
	return l.configHome
}

// Discover returns the config file entries in precedence order.
func (l *ConfigLoader) Discover() []ConfigEntry {
	// User legacy: parent of configHome/.claw.json
	userLegacyPath := filepath.Join(filepath.Dir(l.configHome), ".claw.json")
	return []ConfigEntry{
		{Source: SourceUser, Path: userLegacyPath},
		{Source: SourceUser, Path: filepath.Join(l.configHome, "settings.json")},
		{Source: SourceProject, Path: filepath.Join(l.cwd, ".claw.json")},
		{Source: SourceProject, Path: filepath.Join(l.cwd, ".claw", "settings.json")},
		{Source: SourceLocal, Path: filepath.Join(l.cwd, ".claw", "settings.local.json")},
	}
}

// Load discovers, reads, merges all config files and returns a Config.
func (l *ConfigLoader) Load() (*Config, error) {
	merged := make(map[string]interface{})
	var loadedEntries []ConfigEntry
	var valueEntries []ConfigValueEntry
	mcpServers := make(map[string]ScopedMcpServerConfig)

	for _, entry := range l.Discover() {
		value, err := readOptionalJSONObject(entry.Path)
		if err != nil {
			return nil, err
		}
		if value == nil {
			continue
		}
		sourceName := strings.ToLower(entry.Source.String())
		mergeMCPServers(&mcpServers, entry.Source, value, entry.Path)
		// Record individual config values before merge
		recordValueEntries(&valueEntries, value, sourceName, "")
		deepMergeInterfaces(merged, value)
		loadedEntries = append(loadedEntries, entry)
	}

	cfg := &Config{
		PermissionMode: "danger-full-access",
		MCPServers:     make(map[string]MCPServer),
		Settings:       make(map[string]interface{}),
		loadedEntries:  loadedEntries,
		valueEntries:   valueEntries,
		merged:         merged,
		mcpCollection:  McpConfigCollection{Servers: mcpServers},
	}

	// Extract typed config from merged
	parseModelFromMerged(cfg, merged)
	parsePermissionModeFromMerged(cfg, merged)
	parseHooksFromMerged(cfg, merged)
	parseMCPServersFromMerged(cfg, merged)
	parsePluginsFromMerged(cfg, merged)
	parseSandboxFromMerged(cfg, merged)
	parseSettingsFromMerged(cfg, merged)
	parseOAuthFromMerged(cfg, merged)

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Set defaults
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}
	if !cfg.Features.Streaming {
		cfg.Features.Streaming = true
	}

	return cfg, nil
}

// --- Top-level Load function (backward-compatible) ---

// Load discovers and merges configuration from all layers using deep merge.
func Load() (*Config, error) {
	cwd, _ := os.Getwd()
	loader := DefaultConfigLoader(cwd)
	return loader.Load()
}

// --- defaultConfigHome ---

func defaultConfigHome() string {
	if home := os.Getenv("CLAW_CONFIG_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".go-claw")
	}
	return ".go-claw"
}

// --- File reading ---

func readOptionalJSONObject(path string) (map[string]interface{}, error) {
	isLegacy := filepath.Base(path) == ".claw.json"

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &ConfigError{Kind: "io", Message: fmt.Sprintf("read %s", path), Cause: err}
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return make(map[string]interface{}), nil
	}

	var raw interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		if isLegacy {
			return nil, nil
		}
		return nil, &ConfigError{Kind: "parse", Message: fmt.Sprintf("%s: %v", path, err)}
	}

	obj, ok := raw.(map[string]interface{})
	if !ok {
		if isLegacy {
			return nil, nil
		}
		return nil, &ConfigError{
			Kind:    "parse",
			Message: fmt.Sprintf("%s: top-level settings value must be a JSON object", path),
		}
	}
	return obj, nil
}

// --- MCP server parsing ---

func mergeMCPServers(target *map[string]ScopedMcpServerConfig, source ConfigSource, root map[string]interface{}, path string) {
	mcpServers, ok := root["mcpServers"].(map[string]interface{})
	if !ok {
		return
	}
	for name, value := range mcpServers {
		sm, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		parsed := parseMCPServerConfig(sm, fmt.Sprintf("%s: mcpServers.%s", path, name))
		(*target)[name] = ScopedMcpServerConfig{
			Scope:  source,
			Config: parsed,
		}
	}
}

func parseMCPServerConfig(obj map[string]interface{}, context string) McpServerConfig {
	serverType := strValDefault(obj, "type", "stdio")

	switch serverType {
	case "stdio":
		return McpServerConfig{
			Transport: McpTransportStdio,
			Stdio: &McpStdioServerConfig{
				Command: strValOr(obj, "command", ""),
				Args:    stringSliceVal(obj, "args"),
				Env:     stringMapVal(obj, "env"),
			},
		}
	case "sse":
		return McpServerConfig{
			Transport: McpTransportSse,
			Remote:    parseMcpRemoteServerConfig(obj, context),
		}
	case "http":
		return McpServerConfig{
			Transport: McpTransportHttp,
			Remote:    parseMcpRemoteServerConfig(obj, context),
		}
	case "ws":
		return McpServerConfig{
			Transport: McpTransportWs,
			WebSocket: &McpWebSocketServerConfig{
				URL:           strValOr(obj, "url", ""),
				Headers:       stringMapVal(obj, "headers"),
				HeadersHelper: strValOr(obj, "headersHelper", ""),
			},
		}
	case "sdk":
		return McpServerConfig{
			Transport: McpTransportSdk,
			SDK: &McpSdkServerConfig{
				Name: strValOr(obj, "name", ""),
			},
		}
	case "claudeai-proxy":
		return McpServerConfig{
			Transport:    McpTransportManagedProxy,
			ManagedProxy: &McpManagedProxyServerConfig{
				URL: strValOr(obj, "url", ""),
				ID:  strValOr(obj, "id", ""),
			},
		}
	default:
		return McpServerConfig{Transport: McpTransportStdio}
	}
}

func parseMcpRemoteServerConfig(obj map[string]interface{}, context string) *McpRemoteServerConfig {
	cfg := &McpRemoteServerConfig{
		URL:           strValOr(obj, "url", ""),
		Headers:       stringMapVal(obj, "headers"),
		HeadersHelper: strValOr(obj, "headersHelper", ""),
	}
	// Parse optional OAuth config
	if oauthObj, ok := obj["oauth"].(map[string]interface{}); ok {
		cfg.OAuth = &McpOAuthConfig{
			ClientID:   strValOr(oauthObj, "clientId", ""),
			Xaa:        boolValPtr(oauthObj, "xaa"),
		}
		if port := uint16ValPtr(oauthObj, "callbackPort"); port != nil {
			cfg.OAuth.CallbackPort = port
		}
		if v := strValOr(oauthObj, "authServerMetadataUrl", ""); v != "" {
			cfg.OAuth.AuthServerMetadataURL = v
		}
	}
	return cfg
}

// --- Merged data parsing ---

func parseModelFromMerged(cfg *Config, merged map[string]interface{}) {
	if v, ok := strVal(merged, "model"); ok && v != "" {
		cfg.Model = v
	}
}

func parsePermissionModeFromMerged(cfg *Config, merged map[string]interface{}) {
	if mode, ok := strVal(merged, "permissionMode"); ok {
		cfg.PermissionMode = mode
		if resolved, err := parsePermissionModeLabel(mode); err == nil {
			cfg.resolvedPerm = &resolved
		}
		return
	}
	// Also check permissions.defaultMode
	if perms, ok := merged["permissions"].(map[string]interface{}); ok {
		if dm, ok := strVal(perms, "defaultMode"); ok {
			cfg.PermissionMode = dm
			if resolved, err := parsePermissionModeLabel(dm); err == nil {
				cfg.resolvedPerm = &resolved
			}
		}
	}
}

func parsePermissionModeLabel(mode string) (ResolvedPermissionMode, error) {
	switch mode {
	case "default", "plan", "read-only":
		return PermReadOnly, nil
	case "acceptEdits", "auto", "workspace-write":
		return PermWorkspaceWrite, nil
	case "dontAsk", "danger-full-access":
		return PermDangerFullAccess, nil
	default:
		return PermDangerFullAccess, fmt.Errorf("unsupported permission mode: %s", mode)
	}
}

func parseHooksFromMerged(cfg *Config, merged map[string]interface{}) {
	hooks, ok := merged["hooks"].(map[string]interface{})
	if !ok {
		return
	}
	cfg.Hooks.PreToolUse = stringSliceVal(hooks, "PreToolUse")
	cfg.Hooks.PostToolUse = stringSliceVal(hooks, "PostToolUse")
	cfg.Hooks.Notification = stringSliceVal(hooks, "Notification")
	cfg.Hooks.Stop = stringSliceVal(hooks, "Stop")
	cfg.Hooks.SubagentBefore = stringSliceVal(hooks, "SubagentBefore")
	cfg.Hooks.SubagentAfter = stringSliceVal(hooks, "SubagentAfter")
}

func parseMCPServersFromMerged(cfg *Config, merged map[string]interface{}) {
	servers, ok := merged["mcpServers"].(map[string]interface{})
	if !ok {
		return
	}
	for name, srv := range servers {
		sm, ok := srv.(map[string]interface{})
		if !ok {
			continue
		}
		server := MCPServer{
			Type:    strValDefault(sm, "type", "stdio"),
			Command: strValOr(sm, "command", ""),
			URL:     strValOr(sm, "url", ""),
		}
		server.Args = stringSliceVal(sm, "args")
		server.Headers = stringMapVal(sm, "headers")
		server.Env = stringMapVal(sm, "env")
		if rawCfg, ok := sm["config"].(map[string]interface{}); ok {
			server.Config = rawCfg
		}
		cfg.MCPServers[name] = server
	}
}

func parsePluginsFromMerged(cfg *Config, merged map[string]interface{}) {
	cfg.pluginConfig = RuntimePluginConfig{
		EnabledPlugins: make(map[string]bool),
	}

	// Top-level enabledPlugins (map of bool)
	if enabledPlugins, ok := merged["enabledPlugins"].(map[string]interface{}); ok {
		for k, v := range enabledPlugins {
			if b, ok := v.(bool); ok {
				cfg.pluginConfig.EnabledPlugins[k] = b
			}
		}
	}

	// plugins.enabled
	plugins, ok := merged["plugins"].(map[string]interface{})
	if !ok {
		return
	}
	if enabled, ok := plugins["enabled"].(map[string]interface{}); ok {
		for k, v := range enabled {
			if b, ok := v.(bool); ok {
				cfg.pluginConfig.EnabledPlugins[k] = b
			}
		}
	}
	// Legacy: plugins.enabled as string array (backward compat)
	if enabledList, ok := plugins["enabled"].([]interface{}); ok {
		for _, v := range enabledList {
			if s, ok := v.(string); ok {
				cfg.Plugins.Enabled = append(cfg.Plugins.Enabled, s)
			}
		}
	}
	cfg.pluginConfig.ExternalDirectories = stringSliceVal(plugins, "externalDirectories")
	cfg.pluginConfig.InstallRoot = strValOr(plugins, "installRoot", "")
	cfg.pluginConfig.RegistryPath = strValOr(plugins, "registryPath", "")
	cfg.pluginConfig.BundledRoot = strValOr(plugins, "bundledRoot", "")
}

func parseSandboxFromMerged(cfg *Config, merged map[string]interface{}) {
	sb, ok := merged["sandbox"].(map[string]interface{})
	if !ok {
		return
	}
	if v, ok := sb["enabled"].(bool); ok {
		cfg.Sandbox.Enabled = v
	}
	if v, ok := sb["allowNet"].(bool); ok {
		cfg.Sandbox.AllowNet = v
	}
	cfg.Sandbox.AllowPaths = stringSliceVal(sb, "allowPaths")
	if v, ok := sb["isolation"].(float64); ok {
		cfg.Sandbox.Isolation = int(v)
	}
	if v, ok := sb["namespaceRestrictions"].(bool); ok {
		cfg.Sandbox.NamespaceRestrictions = &v
	}
	if v, ok := sb["networkIsolation"].(bool); ok {
		cfg.Sandbox.NetworkIsolation = &v
	}
	if v, ok := sb["filesystemMode"].(string); ok {
		cfg.Sandbox.FilesystemMode = v
	}
	cfg.Sandbox.AllowedMounts = stringSliceVal(sb, "allowedMounts")
}

func parseSettingsFromMerged(cfg *Config, merged map[string]interface{}) {
	if settings, ok := merged["settings"].(map[string]interface{}); ok {
		cfg.Settings = deepMergeMaps(cfg.Settings, settings)
	}
}

func parseOAuthFromMerged(cfg *Config, merged map[string]interface{}) {
	oauthObj, ok := merged["oauth"].(map[string]interface{})
	if !ok {
		return
	}
	oauth := &OAuthConfig{
		ClientID:   strValOr(oauthObj, "clientId", ""),
		AuthorizeURL: strValOr(oauthObj, "authorizeUrl", ""),
		TokenURL:   strValOr(oauthObj, "tokenUrl", ""),
	}
	oauth.CallbackPort = uint16ValPtr(oauthObj, "callbackPort")
	oauth.ManualRedirectURL = strValOr(oauthObj, "manualRedirectUrl", "")
	oauth.Scopes = stringSliceVal(oauthObj, "scopes")
	cfg.oauth = oauth
}

// --- Environment variable overrides ---

func applyEnvOverrides(cfg *Config) {
	if m := os.Getenv("CLAW_MODEL"); m != "" {
		cfg.Model = m
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "model", Value: m, Source: "env:CLAW_MODEL"})
	}
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		cfg.Model = m
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "model", Value: m, Source: "env:ANTHROPIC_MODEL"})
	}
	if p := os.Getenv("CLAW_PERMISSION_MODE"); p != "" {
		cfg.PermissionMode = p
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "permissionMode", Value: p, Source: "env:CLAW_PERMISSION_MODE"})
	}
	if m := os.Getenv("CLAW_SANDBOX_ENABLED"); m == "true" || m == "1" {
		cfg.Sandbox.Enabled = true
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "sandbox.enabled", Value: m, Source: "env:CLAW_SANDBOX_ENABLED"})
	}
	if m := os.Getenv("CLAW_SANDBOX_NETWORK"); m == "true" || m == "1" {
		cfg.Sandbox.AllowNet = true
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "sandbox.allowNet", Value: m, Source: "env:CLAW_SANDBOX_NETWORK"})
	}
	if m := os.Getenv("CLAW_SANDBOX_ISOLATION"); m != "" {
		cfg.valueEntries = append(cfg.valueEntries, ConfigValueEntry{Key: "sandbox.isolation", Value: m, Source: "env:CLAW_SANDBOX_ISOLATION"})
		switch m {
		case "path":
			cfg.Sandbox.Isolation = 1
		case "env":
			cfg.Sandbox.Isolation = 2
		case "namespace":
			cfg.Sandbox.Isolation = 3
		}
	}
}

// --- Deep merge ---

// deepMergeInterfaces recursively merges src into dst (both must be map[string]interface{}).
func deepMergeInterfaces(dst, src map[string]interface{}) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]interface{}); ok {
			if dstMap, ok := dst[k].(map[string]interface{}); ok {
				deepMergeInterfaces(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}

// deepMergeMaps recursively merges src into dst.
func deepMergeMaps(dst, src map[string]interface{}) map[string]interface{} {
	deepMergeInterfaces(dst, src)
	return dst
}

// extendUnique appends values from src to dst, skipping duplicates.
func extendUnique(dst *[]string, src []string) {
	for _, v := range src {
		pushUnique(dst, v)
	}
}

// pushUnique appends a value if not already present.
func pushUnique(dst *[]string, value string) {
	for _, existing := range *dst {
		if existing == value {
			return
		}
	}
	*dst = append(*dst, value)
}

// --- Value extraction helpers ---

func strVal(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func strValOr(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func strValDefault(m map[string]interface{}, key, def string) string {
	return strValOr(m, key, def)
}

func stringSliceVal(m map[string]interface{}, key string) []string {
	arr, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func stringMapVal(m map[string]interface{}, key string) map[string]string {
	obj, ok := m[key].(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]string)
	for k, v := range obj {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}

func boolValPtr(m map[string]interface{}, key string) *bool {
	if v, ok := m[key].(bool); ok {
		return &v
	}
	return nil
}

func uint16ValPtr(m map[string]interface{}, key string) *uint16 {
	if v, ok := m[key].(float64); ok {
		n := uint16(v)
		return &n
	}
	return nil
}

// --- Save ---

// Save writes config to the project settings file.
func Save(cfg *Config) error {
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, ".claw")
	os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)
}

// --- DescribeSources ---

// recordValueEntries walks a JSON object and records each leaf value as a ConfigValueEntry.
func recordValueEntries(entries *[]ConfigValueEntry, obj map[string]interface{}, source, prefix string) {
	for k, v := range obj {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			recordValueEntries(entries, val, source, fullKey)
		case []interface{}:
			for i, elem := range val {
				if s, ok := elem.(string); ok {
					*entries = append(*entries, ConfigValueEntry{
						Key:    fmt.Sprintf("%s[%d]", fullKey, i),
						Value:  s,
						Source: source,
					})
				} else {
					*entries = append(*entries, ConfigValueEntry{
						Key:    fullKey,
						Value:  fmt.Sprintf("%v", elem),
						Source: source,
					})
				}
			}
		default:
			*entries = append(*entries, ConfigValueEntry{
				Key:    fullKey,
				Value:  fmt.Sprintf("%v", v),
				Source: source,
			})
		}
	}
}

// DescribeSources formats the loaded config entries for display, showing
// each config key, its value, and which source provided it.
func (c *Config) DescribeSources() string {
	var buf strings.Builder
	buf.WriteString("Configuration sources:\n")
	for _, e := range c.valueEntries {
		fmt.Fprintf(&buf, "  %-40s = %-30s [%s]\n", e.Key, e.Value, e.Source)
	}
	return buf.String()
}

// DescribeSources returns a human-readable list of config sources and their status.
func DescribeSources() string {
	cwd, _ := os.Getwd()
	loader := DefaultConfigLoader(cwd)
	entries := loader.Discover()
	var buf strings.Builder
	for _, entry := range entries {
		if _, err := os.Stat(entry.Path); err == nil {
			fmt.Fprintf(&buf, "  [ok] %s (%s): %s\n", entry.Source, sourceLabel(entry.Source), entry.Path)
		} else {
			fmt.Fprintf(&buf, "  [--] %s (%s): (not found)\n", entry.Source, sourceLabel(entry.Source))
		}
	}
	return buf.String()
}

func sourceLabel(source ConfigSource) string {
	switch source {
	case SourceUser:
		return "User"
	case SourceProject:
		return "Project"
	case SourceLocal:
		return "Local"
	default:
		return "Unknown"
	}
}

// Ensure sorted import for potential future use
var _ = sort.Strings
