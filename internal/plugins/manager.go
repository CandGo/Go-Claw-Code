package plugins

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// PermissionChecker is a function that checks if a permission level is allowed.
// Returns true if the requested permission is allowed by the current mode.
type PermissionChecker func(required PluginToolPermission) bool

var globalPermissionChecker PermissionChecker

// SetPermissionChecker sets the global permission checker for plugin tools.
func SetPermissionChecker(checker PermissionChecker) {
	globalPermissionChecker = checker
}

// --- Constants ---

const (
	externalMarketplace   = "external"
	builtinMarketplace    = "builtin"
	bundledMarketplace    = "bundled"
	settingsFileName      = "settings.json"
	registryFileName      = "installed.json"
	manifestFileName      = "plugin.json"
	manifestRelativePath  = ".claw-plugin/plugin.json"
)

// --- PluginKind ---

// PluginKind mirrors Rust PluginKind.
type PluginKind int

const (
	KindBuiltin PluginKind = iota
	KindBundled
	KindExternal
)

func (k PluginKind) String() string {
	switch k {
	case KindBuiltin:
		return "builtin"
	case KindBundled:
		return "bundled"
	case KindExternal:
		return "external"
	default:
		return "unknown"
	}
}

func (k PluginKind) marketplace() string {
	switch k {
	case KindBuiltin:
		return builtinMarketplace
	case KindBundled:
		return bundledMarketplace
	default:
		return externalMarketplace
	}
}

// --- PluginPermission ---

// PluginPermission mirrors Rust PluginPermission.
type PluginPermission string

const (
	PermRead    PluginPermission = "read"
	PermWrite   PluginPermission = "write"
	PermExecute PluginPermission = "execute"
)

func parsePluginPermission(s string) (PluginPermission, bool) {
	switch s {
	case "read":
		return PermRead, true
	case "write":
		return PermWrite, true
	case "execute":
		return PermExecute, true
	default:
		return "", false
	}
}

// --- PluginToolPermission ---

// PluginToolPermission mirrors Rust PluginToolPermission.
type PluginToolPermission int

const (
	ToolPermReadOnly        PluginToolPermission = iota
	ToolPermWorkspaceWrite
	ToolPermDangerFullAccess
)

func (p PluginToolPermission) String() string {
	switch p {
	case ToolPermReadOnly:
		return "read-only"
	case ToolPermWorkspaceWrite:
		return "workspace-write"
	case ToolPermDangerFullAccess:
		return "danger-full-access"
	default:
		return "unknown"
	}
}

func parsePluginToolPermission(s string) (PluginToolPermission, bool) {
	switch s {
	case "read-only":
		return ToolPermReadOnly, true
	case "workspace-write":
		return ToolPermWorkspaceWrite, true
	case "danger-full-access":
		return ToolPermDangerFullAccess, true
	default:
		return 0, false
	}
}

// --- PluginMetadata ---

// PluginMetadata mirrors Rust PluginMetadata.
type PluginMetadata struct {
	ID            string
	Name          string
	Version       string
	Description   string
	Kind          PluginKind
	Source        string
	DefaultEnabled bool
	Root          string // optional, empty means none
}

// --- PluginLifecycle ---

// PluginLifecycle mirrors Rust PluginLifecycle.
type PluginLifecycle struct {
	Init     []string `json:"Init"`
	Shutdown []string `json:"Shutdown"`
}

func (l *PluginLifecycle) IsEmpty() bool {
	return len(l.Init) == 0 && len(l.Shutdown) == 0
}

// --- PluginHooks ---

// PluginHooks mirrors Rust PluginHooks.
type PluginHooks struct {
	PreToolUse  []string `json:"PreToolUse"`
	PostToolUse []string `json:"PostToolUse"`
}

func (h *PluginHooks) IsEmpty() bool {
	return len(h.PreToolUse) == 0 && len(h.PostToolUse) == 0
}

func (h *PluginHooks) MergedWith(other *PluginHooks) PluginHooks {
	merged := PluginHooks{
		PreToolUse:  make([]string, len(h.PreToolUse)),
		PostToolUse: make([]string, len(h.PostToolUse)),
	}
	copy(merged.PreToolUse, h.PreToolUse)
	copy(merged.PostToolUse, h.PostToolUse)
	merged.PreToolUse = appendUnique(merged.PreToolUse, other.PreToolUse)
	merged.PostToolUse = appendUnique(merged.PostToolUse, other.PostToolUse)
	return merged
}

// appendUnique appends strings from src to dst, skipping duplicates.
func appendUnique(dst []string, src []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range src {
		if !seen[s] {
			dst = append(dst, s)
			seen[s] = true
		}
	}
	return dst
}

// --- PluginManifest ---

// PluginManifest mirrors Rust PluginManifest — the validated manifest.
type PluginManifest struct {
	Name           string
	Version        string
	Description    string
	Permissions    []PluginPermission
	DefaultEnabled bool
	Hooks          PluginHooks
	Lifecycle      PluginLifecycle
	Tools          []PluginToolManifest
	Commands       []PluginCommandManifest
}

// PluginToolManifest mirrors Rust PluginToolManifest.
type PluginToolManifest struct {
	Name               string
	Description        string
	InputSchema        interface{}
	Command            string
	Args               []string
	RequiredPermission PluginToolPermission
}

// PluginCommandManifest mirrors Rust PluginCommandManifest.
type PluginCommandManifest struct {
	Name        string
	Description string
	Command     string
}

// PluginToolDefinition mirrors Rust PluginToolDefinition.
type PluginToolDefinition struct {
	Name        string
	Description string
	InputSchema interface{}
}

// --- PluginInstallSource ---

// PluginInstallSource mirrors Rust PluginInstallSource.
type PluginInstallSource struct {
	Type     string // "local_path" or "git_url"
	Path     string // for local_path
	URL      string // for git_url
}

// --- PluginTool ---

// PluginTool mirrors Rust PluginTool — a resolved, executable tool.
type PluginTool struct {
	PluginID           string
	PluginName         string
	Definition         PluginToolDefinition
	Command            string
	Args               []string
	RequiredPermission PluginToolPermission
	Root               string
}

// NewPluginTool creates a new PluginTool.
func NewPluginTool(pluginID, pluginName string, def PluginToolDefinition, command string, args []string, perm PluginToolPermission, root string) PluginTool {
	return PluginTool{
		PluginID: pluginID, PluginName: pluginName, Definition: def,
		Command: command, Args: args, RequiredPermission: perm, Root: root,
	}
}

func (t *PluginTool) RequiredPermissionStr() string {
	return t.RequiredPermission.String()
}

// Execute runs the plugin tool command.
// Mirrors Rust PluginTool::execute.
func (t *PluginTool) Execute(input interface{}) (string, error) {
	// Check permission level
	if globalPermissionChecker != nil && !globalPermissionChecker(t.RequiredPermission) {
		return "", fmt.Errorf("plugin tool `%s` from `%s` requires %s permission",
			t.Definition.Name, t.PluginID, t.RequiredPermission.String())
	}
	inputJSON, _ := json.Marshal(input)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", t.Command)
	} else {
		cmd = exec.Command("sh", "-c", t.Command)
	}
	cmd.Args = append(cmd.Args, t.Args...)
	cmd.Stdin = strings.NewReader(string(inputJSON))
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(),
		"CLAW_PLUGIN_ID="+t.PluginID,
		"CLAW_PLUGIN_NAME="+t.PluginName,
		"CLAW_TOOL_NAME="+t.Definition.Name,
		"CLAW_TOOL_INPUT="+string(inputJSON),
	)
	if t.Root != "" {
		cmd.Dir = t.Root
		cmd.Env = append(cmd.Env, "CLAW_PLUGIN_ROOT="+t.Root)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		stderr := strings.TrimSpace(string(out))
		if stderr == "" {
			return "", fmt.Errorf("plugin tool `%s` from `%s` failed: %v", t.Definition.Name, t.PluginID, err)
		}
		return "", fmt.Errorf("plugin tool `%s` from `%s` failed: %s", t.Definition.Name, t.PluginID, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// --- InstalledPluginRecord ---

// InstalledPluginRecord mirrors Rust InstalledPluginRecord.
type InstalledPluginRecord struct {
	Kind          PluginKind       `json:"kind"`
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Description   string           `json:"description"`
	InstallPath   string           `json:"install_path"`
	Source        PluginInstallSource `json:"source"`
	InstalledAtMs int64            `json:"installed_at_unix_ms"`
	UpdatedAtMs   int64            `json:"updated_at_unix_ms"`
}

// --- InstalledPluginRegistry ---

// InstalledPluginRegistry mirrors Rust InstalledPluginRegistry.
type InstalledPluginRegistry struct {
	Plugins map[string]InstalledPluginRecord `json:"plugins"`
}

// --- PluginError ---

// PluginError mirrors Rust PluginError.
type PluginError struct {
	Kind    PluginErrorKind
	Message string
	Err     error
}

type PluginErrorKind int

const (
	PluginErrIO PluginErrorKind = iota
	PluginErrJSON
	PluginErrManifestValidation
	PluginErrInvalidManifest
	PluginErrNotFound
	PluginErrCommandFailed
)

func (e *PluginError) Error() string {
	switch e.Kind {
	case PluginErrIO:
		return e.Err.Error()
	case PluginErrJSON:
		return e.Err.Error()
	case PluginErrManifestValidation:
		return e.Message
	default:
		return e.Message
	}
}

func (e *PluginError) Unwrap() error { return e.Err }

func pluginErrIO(err error) *PluginError          { return &PluginError{Kind: PluginErrIO, Err: err} }
func pluginErrJSON(err error) *PluginError         { return &PluginError{Kind: PluginErrJSON, Err: err} }
func pluginErrValidation(msg string) *PluginError   { return &PluginError{Kind: PluginErrManifestValidation, Message: msg} }
func pluginErrInvalid(msg string) *PluginError      { return &PluginError{Kind: PluginErrInvalidManifest, Message: msg} }
func pluginErrNotFound(msg string) *PluginError     { return &PluginError{Kind: PluginErrNotFound, Message: msg} }
func pluginErrCommand(msg string) *PluginError      { return &PluginError{Kind: PluginErrCommandFailed, Message: msg} }

// --- ManifestValidationError ---

// ManifestValidationError mirrors Rust PluginManifestValidationError.
type ManifestValidationError struct {
	Type    string // EmptyField, EmptyEntryField, InvalidPermission, DuplicatePermission, DuplicateEntry, MissingPath, InvalidToolInputSchema, InvalidToolRequiredPermission
	Kind    string // "hook", "tool", "command", "lifecycle command", "permission"
	Field   string
	Name    string // optional entry name
	Path    string // optional path
}

func (e ManifestValidationError) Error() string {
	switch e.Type {
	case "EmptyField":
		return fmt.Sprintf("plugin manifest %s cannot be empty", e.Field)
	case "EmptyEntryField":
		if e.Name != "" {
			return fmt.Sprintf("plugin %s `%s` %s cannot be empty", e.Kind, e.Name, e.Field)
		}
		return fmt.Sprintf("plugin %s %s cannot be empty", e.Kind, e.Field)
	case "InvalidPermission":
		return fmt.Sprintf("plugin manifest permission `%s` must be one of read, write, or execute", e.Field)
	case "DuplicatePermission":
		return fmt.Sprintf("plugin manifest permission `%s` is duplicated", e.Field)
	case "DuplicateEntry":
		return fmt.Sprintf("plugin %s `%s` is duplicated", e.Kind, e.Name)
	case "MissingPath":
		return fmt.Sprintf("%s path `%s` does not exist", e.Kind, e.Path)
	case "InvalidToolInputSchema":
		return fmt.Sprintf("plugin tool `%s` inputSchema must be a JSON object", e.Name)
	case "InvalidToolRequiredPermission":
		return fmt.Sprintf("plugin tool `%s` requiredPermission `%s` must be read-only, workspace-write, or danger-full-access", e.Name, e.Field)
	default:
		return e.Field
	}
}

// --- PluginDefinition (sum type via interface) ---

// PluginDefinition mirrors Rust PluginDefinition — a loaded plugin.
type PluginDefinition interface {
	Metadata() *PluginMetadata
	Hooks() *PluginHooks
	Lifecycle() *PluginLifecycle
	Tools() []PluginTool
	Validate() error
	Initialize() error
	Shutdown() error
	Kind() PluginKind
}

// basePlugin holds shared state for all plugin types.
type basePlugin struct {
	metadata  PluginMetadata
	hooks     PluginHooks
	lifecycle PluginLifecycle
	tools     []PluginTool
}

func (b *basePlugin) Metadata() *PluginMetadata  { return &b.metadata }
func (b *basePlugin) Hooks() *PluginHooks         { return &b.hooks }
func (b *basePlugin) Lifecycle() *PluginLifecycle { return &b.lifecycle }
func (b *basePlugin) Tools() []PluginTool         { return b.tools }

// --- BuiltinPlugin ---

// BuiltinPlugin mirrors Rust BuiltinPlugin.
type BuiltinPlugin struct{ basePlugin }

func (p *BuiltinPlugin) Kind() PluginKind { return KindBuiltin }
func (p *BuiltinPlugin) Validate() error  { return nil }
func (p *BuiltinPlugin) Initialize() error { return nil }
func (p *BuiltinPlugin) Shutdown() error   { return nil }

// --- BundledPlugin ---

// BundledPlugin mirrors Rust BundledPlugin.
type BundledPlugin struct{ basePlugin }

func (p *BundledPlugin) Kind() PluginKind { return KindBundled }
func (p *BundledPlugin) Validate() error {
	return validatePluginPaths(&p.metadata, &p.hooks, &p.lifecycle, p.tools)
}
func (p *BundledPlugin) Initialize() error {
	return runLifecycleCommands(&p.metadata, "init", p.lifecycle.Init)
}
func (p *BundledPlugin) Shutdown() error {
	return runLifecycleCommands(&p.metadata, "shutdown", p.lifecycle.Shutdown)
}

// --- ExternalPlugin ---

// ExternalPlugin mirrors Rust ExternalPlugin.
type ExternalPlugin struct{ basePlugin }

func (p *ExternalPlugin) Kind() PluginKind { return KindExternal }
func (p *ExternalPlugin) Validate() error {
	return validatePluginPaths(&p.metadata, &p.hooks, &p.lifecycle, p.tools)
}
func (p *ExternalPlugin) Initialize() error {
	return runLifecycleCommands(&p.metadata, "init", p.lifecycle.Init)
}
func (p *ExternalPlugin) Shutdown() error {
	return runLifecycleCommands(&p.metadata, "shutdown", p.lifecycle.Shutdown)
}

// --- RegisteredPlugin ---

// RegisteredPlugin mirrors Rust RegisteredPlugin — a plugin + enabled state.
type RegisteredPlugin struct {
	Definition PluginDefinition
	Enabled    bool
}

func (r *RegisteredPlugin) Metadata() *PluginMetadata { return r.Definition.Metadata() }
func (r *RegisteredPlugin) Hooks() *PluginHooks       { return r.Definition.Hooks() }
func (r *RegisteredPlugin) Tools() []PluginTool       { return r.Definition.Tools() }
func (r *RegisteredPlugin) IsEnabled() bool            { return r.Enabled }
func (r *RegisteredPlugin) Validate() error            { return r.Definition.Validate() }
func (r *RegisteredPlugin) Initialize() error          { return r.Definition.Initialize() }
func (r *RegisteredPlugin) Shutdown() error            { return r.Definition.Shutdown() }

// Summary returns a PluginSummary.
func (r *RegisteredPlugin) Summary() PluginSummary {
	return PluginSummary{Metadata: *r.Metadata(), Enabled: r.Enabled}
}

// --- PluginSummary ---

// PluginSummary mirrors Rust PluginSummary.
type PluginSummary struct {
	Metadata PluginMetadata
	Enabled  bool
}

// --- PluginRegistry ---

// PluginRegistry mirrors Rust PluginRegistry.
type PluginRegistry struct {
	plugins []RegisteredPlugin
}

// NewPluginRegistry creates a sorted registry from plugins.
// Mirrors Rust PluginRegistry::new.
func NewPluginRegistry(plugins []RegisteredPlugin) PluginRegistry {
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Metadata().ID < plugins[j].Metadata().ID
	})
	return PluginRegistry{plugins: plugins}
}

func (r *PluginRegistry) Plugins() []RegisteredPlugin { return r.plugins }

func (r *PluginRegistry) Get(pluginID string) *RegisteredPlugin {
	for i := range r.plugins {
		if r.plugins[i].Metadata().ID == pluginID {
			return &r.plugins[i]
		}
	}
	return nil
}

func (r *PluginRegistry) Contains(pluginID string) bool { return r.Get(pluginID) != nil }

func (r *PluginRegistry) Summaries() []PluginSummary {
	summaries := make([]PluginSummary, len(r.plugins))
	for i, p := range r.plugins {
		summaries[i] = p.Summary()
	}
	return summaries
}

// AggregatedHooks merges hooks from all enabled plugins.
func (r *PluginRegistry) AggregatedHooks() (PluginHooks, error) {
	var acc PluginHooks
	for i := range r.plugins {
		p := &r.plugins[i]
		if !p.IsEnabled() {
			continue
		}
		if err := p.Validate(); err != nil {
			return acc, err
		}
		acc = acc.MergedWith(p.Hooks())
	}
	return acc, nil
}

// AggregatedTools returns deduplicated tools from enabled plugins.
func (r *PluginRegistry) AggregatedTools() ([]PluginTool, error) {
	var tools []PluginTool
	seenNames := make(map[string]string)
	for i := range r.plugins {
		p := &r.plugins[i]
		if !p.IsEnabled() {
			continue
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		for _, tool := range p.Tools() {
			if existing, ok := seenNames[tool.Definition.Name]; ok {
				return nil, pluginErrInvalid(fmt.Sprintf(
					"plugin tool `%s` is defined by both `%s` and `%s`",
					tool.Definition.Name, existing, tool.PluginID))
			}
			seenNames[tool.Definition.Name] = tool.PluginID
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

// Initialize runs init for all enabled plugins.
func (r *PluginRegistry) Initialize() error {
	for i := range r.plugins {
		p := &r.plugins[i]
		if !p.IsEnabled() {
			continue
		}
		if err := p.Validate(); err != nil {
			return err
		}
		if err := p.Initialize(); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown runs shutdown for all enabled plugins in reverse.
func (r *PluginRegistry) Shutdown() error {
	for i := len(r.plugins) - 1; i >= 0; i-- {
		p := &r.plugins[i]
		if !p.IsEnabled() {
			continue
		}
		if err := p.Shutdown(); err != nil {
			return err
		}
	}
	return nil
}

// --- InstallOutcome / UpdateOutcome ---

// InstallOutcome mirrors Rust InstallOutcome.
type InstallOutcome struct {
	PluginID    string
	Version     string
	InstallPath string
}

// UpdateOutcome mirrors Rust UpdateOutcome.
type UpdateOutcome struct {
	PluginID    string
	OldVersion  string
	NewVersion  string
	InstallPath string
}

// --- PluginManagerConfig ---

// PluginManagerConfig mirrors Rust PluginManagerConfig.
type PluginManagerConfig struct {
	ConfigHome     string
	EnabledPlugins map[string]bool
	ExternalDirs   []string
	InstallRoot    string // optional
	RegistryPath   string // optional
	BundledRoot    string // optional
}

// NewPluginManagerConfig creates a config with defaults.
func NewPluginManagerConfig(configHome string) PluginManagerConfig {
	return PluginManagerConfig{
		ConfigHome:     configHome,
		EnabledPlugins: make(map[string]bool),
	}
}

// --- PluginManager ---

// PluginManager mirrors Rust PluginManager.
type PluginManager struct {
	config PluginManagerConfig
}

// NewManager creates a new PluginManager.
// Mirrors Rust PluginManager::new.
func NewManager(config PluginManagerConfig) *PluginManager {
	return &PluginManager{config: config}
}

func (m *PluginManager) InstallRoot() string {
	if m.config.InstallRoot != "" {
		return m.config.InstallRoot
	}
	return filepath.Join(m.config.ConfigHome, "plugins", "installed")
}

func (m *PluginManager) RegistryPath() string {
	if m.config.RegistryPath != "" {
		return m.config.RegistryPath
	}
	return filepath.Join(m.config.ConfigHome, "plugins", registryFileName)
}

func (m *PluginManager) SettingsPath() string {
	return filepath.Join(m.config.ConfigHome, settingsFileName)
}

// PluginRegistry discovers all plugins and builds a registry.
// Mirrors Rust PluginManager::plugin_registry.
func (m *PluginManager) PluginRegistry() (*PluginRegistry, error) {
	definitions, err := m.DiscoverPlugins()
	if err != nil {
		return nil, err
	}
	var registered []RegisteredPlugin
	for _, def := range definitions {
		enabled := m.isEnabled(def.Metadata())
		registered = append(registered, RegisteredPlugin{Definition: def, Enabled: enabled})
	}
	reg := NewPluginRegistry(registered)
	return &reg, nil
}

// ListPlugins lists all plugin summaries.
func (m *PluginManager) ListPlugins() ([]PluginSummary, error) {
	reg, err := m.PluginRegistry()
	if err != nil {
		return nil, err
	}
	return reg.Summaries(), nil
}

// DiscoverPlugins discovers all plugins.
// Mirrors Rust PluginManager::discover_plugins.
func (m *PluginManager) DiscoverPlugins() ([]PluginDefinition, error) {
	if err := m.syncBundledPlugins(); err != nil {
		return nil, err
	}
	plugins := builtinPlugins()
	installed, err := m.discoverInstalledPlugins()
	if err != nil {
		return nil, err
	}
	plugins = append(plugins, installed...)
	external, err := m.discoverExternalDirectoryPlugins(plugins)
	if err != nil {
		return nil, err
	}
	plugins = append(plugins, external...)
	return plugins, nil
}

// AggregatedHooks returns merged hooks from enabled plugins.
func (m *PluginManager) AggregatedHooks() (PluginHooks, error) {
	reg, err := m.PluginRegistry()
	if err != nil {
		return PluginHooks{}, err
	}
	return reg.AggregatedHooks()
}

// AggregatedTools returns deduplicated tools from enabled plugins.
func (m *PluginManager) AggregatedTools() ([]PluginTool, error) {
	reg, err := m.PluginRegistry()
	if err != nil {
		return nil, err
	}
	return reg.AggregatedTools()
}

// ValidatePluginSource validates a plugin source before install.
func (m *PluginManager) ValidatePluginSource(source string) (*PluginManifest, error) {
	path, err := resolveLocalSource(source)
	if err != nil {
		return nil, err
	}
	return LoadPluginFromDirectory(path)
}

// Install installs a plugin from a local path or git URL.
// Mirrors Rust PluginManager::install.
func (m *PluginManager) Install(source string) (*InstallOutcome, error) {
	installSource, err := parseInstallSource(source)
	if err != nil {
		return nil, err
	}
	tempRoot := filepath.Join(m.InstallRoot(), ".tmp")
	stagedSource, err := materializeSource(&installSource, tempRoot)
	if err != nil {
		return nil, err
	}
	cleanup := installSource.Type == "git_url"

	manifest, err := LoadPluginFromDirectory(stagedSource)
	if err != nil {
		if cleanup {
			os.RemoveAll(stagedSource)
		}
		return nil, err
	}

	pid := pluginID(manifest.Name, externalMarketplace)
	installPath := filepath.Join(m.InstallRoot(), sanitizePluginID(pid))
	if _, err := os.Stat(installPath); err == nil {
		os.RemoveAll(installPath)
	}
	if err := copyDirAll(stagedSource, installPath); err != nil {
		return nil, pluginErrIO(err)
	}
	if cleanup {
		os.RemoveAll(stagedSource)
	}

	now := unixTimeMs()
	record := InstalledPluginRecord{
		Kind: KindExternal, ID: pid, Name: manifest.Name,
		Version: manifest.Version, Description: manifest.Description,
		InstallPath: installPath, Source: installSource,
		InstalledAtMs: now, UpdatedAtMs: now,
	}

	registry, err := m.loadRegistry()
	if err != nil {
		return nil, err
	}
	registry.Plugins[pid] = record
	if err := m.storeRegistry(registry); err != nil {
		return nil, err
	}
	m.writeEnabledState(pid, boolPtr(true))
	m.config.EnabledPlugins[pid] = true

	return &InstallOutcome{PluginID: pid, Version: manifest.Version, InstallPath: installPath}, nil
}

// Enable enables a plugin.
func (m *PluginManager) Enable(pluginID string) error {
	if err := m.ensureKnownPlugin(pluginID); err != nil {
		return err
	}
	m.writeEnabledState(pluginID, boolPtr(true))
	m.config.EnabledPlugins[pluginID] = true
	return nil
}

// Disable disables a plugin.
func (m *PluginManager) Disable(pluginID string) error {
	if err := m.ensureKnownPlugin(pluginID); err != nil {
		return err
	}
	m.writeEnabledState(pluginID, boolPtr(false))
	m.config.EnabledPlugins[pluginID] = false
	return nil
}

// Uninstall removes a plugin.
func (m *PluginManager) Uninstall(pluginID string) error {
	registry, err := m.loadRegistry()
	if err != nil {
		return err
	}
	record, ok := registry.Plugins[pluginID]
	if !ok {
		return pluginErrNotFound(fmt.Sprintf("plugin `%s` is not installed", pluginID))
	}
	if record.Kind == KindBundled {
		return pluginErrCommand(fmt.Sprintf("plugin `%s` is bundled and managed automatically; disable it instead", pluginID))
	}
	if _, err := os.Stat(record.InstallPath); err == nil {
		os.RemoveAll(record.InstallPath)
	}
	delete(registry.Plugins, pluginID)
	m.storeRegistry(registry)
	m.writeEnabledState(pluginID, nil)
	delete(m.config.EnabledPlugins, pluginID)
	return nil
}

// Update updates a plugin to the latest version.
func (m *PluginManager) Update(pluginID string) (*UpdateOutcome, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return nil, err
	}
	record, ok := registry.Plugins[pluginID]
	if !ok {
		return nil, pluginErrNotFound(fmt.Sprintf("plugin `%s` is not installed", pluginID))
	}

	tempRoot := filepath.Join(m.InstallRoot(), ".tmp")
	stagedSource, err := materializeSource(&record.Source, tempRoot)
	if err != nil {
		return nil, err
	}
	cleanup := record.Source.Type == "git_url"

	manifest, err := LoadPluginFromDirectory(stagedSource)
	if err != nil {
		if cleanup {
			os.RemoveAll(stagedSource)
		}
		return nil, err
	}

	if _, err := os.Stat(record.InstallPath); err == nil {
		os.RemoveAll(record.InstallPath)
	}
	copyDirAll(stagedSource, record.InstallPath)
	if cleanup {
		os.RemoveAll(stagedSource)
	}

	oldVersion := record.Version
	record.Version = manifest.Version
	record.Description = manifest.Description
	record.UpdatedAtMs = unixTimeMs()
	registry.Plugins[pluginID] = record
	m.storeRegistry(registry)

	return &UpdateOutcome{
		PluginID: pluginID, OldVersion: oldVersion,
		NewVersion: manifest.Version, InstallPath: record.InstallPath,
	}, nil
}

// --- Private methods ---

func (m *PluginManager) discoverInstalledPlugins() ([]PluginDefinition, error) {
	registry, err := m.loadRegistry()
	if err != nil {
		return nil, err
	}
	var plugins []PluginDefinition
	seenIDs := make(map[string]bool)
	seenPaths := make(map[string]bool)
	var staleIDs []string

	dirs, err := discoverPluginDirs(m.InstallRoot())
	if err != nil {
		return nil, err
	}
	for _, installPath := range dirs {
		kind := KindExternal
		source := installPath
		for _, rec := range registry.Plugins {
			if rec.InstallPath == installPath {
				kind = rec.Kind
				source = describeInstallSource(&rec.Source)
				break
			}
		}
		def, err := loadPluginDefinition(installPath, kind, source, kind.marketplace())
		if err != nil {
			continue
		}
		if !seenIDs[def.Metadata().ID] {
			seenIDs[def.Metadata().ID] = true
			seenPaths[installPath] = true
			plugins = append(plugins, def)
		}
	}

	for pid, record := range registry.Plugins {
		if seenPaths[record.InstallPath] {
			continue
		}
		if _, err := os.Stat(record.InstallPath); os.IsNotExist(err) {
			staleIDs = append(staleIDs, pid)
			continue
		}
		if _, err := pluginManifestPath(record.InstallPath); err != nil {
			staleIDs = append(staleIDs, pid)
			continue
		}
		def, err := loadPluginDefinition(
			record.InstallPath, record.Kind,
			describeInstallSource(&record.Source), record.Kind.marketplace())
		if err != nil {
			continue
		}
		if !seenIDs[def.Metadata().ID] {
			seenIDs[def.Metadata().ID] = true
			plugins = append(plugins, def)
		}
	}

	if len(staleIDs) > 0 {
		for _, pid := range staleIDs {
			delete(registry.Plugins, pid)
		}
		m.storeRegistry(registry)
	}
	return plugins, nil
}

func (m *PluginManager) discoverExternalDirectoryPlugins(existing []PluginDefinition) ([]PluginDefinition, error) {
	var plugins []PluginDefinition
	for _, dir := range m.config.ExternalDirs {
		dirs, err := discoverPluginDirs(dir)
		if err != nil {
			continue
		}
		for _, root := range dirs {
			def, err := loadPluginDefinition(root, KindExternal, root, externalMarketplace)
			if err != nil {
				continue
			}
			duplicate := false
			for _, ex := range existing {
				if ex.Metadata().ID == def.Metadata().ID {
					duplicate = true
					break
				}
			}
			for _, ex := range plugins {
				if ex.Metadata().ID == def.Metadata().ID {
					duplicate = true
					break
				}
			}
			if !duplicate {
				plugins = append(plugins, def)
			}
		}
	}
	return plugins, nil
}

func (m *PluginManager) syncBundledPlugins() error {
	bundledRoot := m.config.BundledRoot
	if bundledRoot == "" {
		bundledRoot = defaultBundledRoot()
	}
	dirs, err := discoverPluginDirs(bundledRoot)
	if err != nil {
		return nil // no bundled plugins dir is fine
	}
	registry, err := m.loadRegistry()
	if err != nil {
		return err
	}
	changed := false
	installRoot := m.InstallRoot()
	activeBundledIDs := make(map[string]bool)

	for _, sourceRoot := range dirs {
		manifest, err := LoadPluginFromDirectory(sourceRoot)
		if err != nil {
			continue
		}
		pid := pluginID(manifest.Name, bundledMarketplace)
		activeBundledIDs[pid] = true
		installPath := filepath.Join(installRoot, sanitizePluginID(pid))
		now := unixTimeMs()

		existing, exists := registry.Plugins[pid]
		needsSync := !exists || existing.Kind != KindBundled ||
			existing.Version != manifest.Version || existing.Name != manifest.Name ||
			existing.Description != manifest.Description ||
			existing.InstallPath != installPath

		if !needsSync {
			continue
		}

		os.RemoveAll(installPath)
		copyDirAll(sourceRoot, installPath)

		installedAt := now
		if exists {
			installedAt = existing.InstalledAtMs
		}
		registry.Plugins[pid] = InstalledPluginRecord{
			Kind: KindBundled, ID: pid, Name: manifest.Name,
			Version: manifest.Version, Description: manifest.Description,
			InstallPath: installPath,
			Source: PluginInstallSource{Type: "local_path", Path: sourceRoot},
			InstalledAtMs: installedAt, UpdatedAtMs: now,
		}
		changed = true
	}

	for pid, record := range registry.Plugins {
		if record.Kind == KindBundled && !activeBundledIDs[pid] {
			os.RemoveAll(record.InstallPath)
			delete(registry.Plugins, pid)
			changed = true
		}
	}

	if changed {
		m.storeRegistry(registry)
	}
	return nil
}

func (m *PluginManager) isEnabled(meta *PluginMetadata) bool {
	if enabled, ok := m.config.EnabledPlugins[meta.ID]; ok {
		return enabled
	}
	switch meta.Kind {
	case KindExternal:
		return false
	default:
		return meta.DefaultEnabled
	}
}

func (m *PluginManager) ensureKnownPlugin(pluginID string) error {
	reg, err := m.PluginRegistry()
	if err != nil {
		return err
	}
	if !reg.Contains(pluginID) {
		return pluginErrNotFound(fmt.Sprintf("plugin `%s` is not installed or discoverable", pluginID))
	}
	return nil
}

func (m *PluginManager) loadRegistry() (*InstalledPluginRegistry, error) {
	path := m.RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstalledPluginRegistry{Plugins: make(map[string]InstalledPluginRecord)}, nil
		}
		return nil, pluginErrIO(err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &InstalledPluginRegistry{Plugins: make(map[string]InstalledPluginRecord)}, nil
	}
	var reg InstalledPluginRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, pluginErrJSON(err)
	}
	if reg.Plugins == nil {
		reg.Plugins = make(map[string]InstalledPluginRecord)
	}
	return &reg, nil
}

func (m *PluginManager) storeRegistry(registry *InstalledPluginRegistry) error {
	path := m.RegistryPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return pluginErrJSON(err)
	}
	return os.WriteFile(path, data, 0644)
}

func (m *PluginManager) writeEnabledState(pluginID string, enabled *bool) {
	updateSettingsJSON(m.SettingsPath(), func(root map[string]interface{}) {
		enabledPlugins, _ := root["enabledPlugins"].(map[string]interface{})
		if enabledPlugins == nil {
			enabledPlugins = make(map[string]interface{})
			root["enabledPlugins"] = enabledPlugins
		}
		if enabled != nil {
			enabledPlugins[pluginID] = *enabled
		} else {
			delete(enabledPlugins, pluginID)
		}
	})
}

// --- Free functions ---

// BuiltinPlugins returns the list of built-in plugins.
func BuiltinPlugins() []PluginDefinition {
	return []PluginDefinition{
		&BuiltinPlugin{basePlugin{
			metadata: PluginMetadata{
				ID: pluginID("example-builtin", builtinMarketplace),
				Name: "example-builtin", Version: "0.1.0",
				Description: "Example built-in plugin scaffold for the plugin system",
				Kind: KindBuiltin, Source: builtinMarketplace,
			},
		}},
	}
}

// kept for backward compatibility
func builtinPlugins() []PluginDefinition { return BuiltinPlugins() }

// LoadPluginFromDirectory loads and validates a plugin manifest from a directory.
func LoadPluginFromDirectory(root string) (*PluginManifest, error) {
	manifestPath, err := pluginManifestPath(root)
	if err != nil {
		return nil, err
	}
	return loadManifestFromPath(root, manifestPath)
}

func loadPluginDefinition(root string, kind PluginKind, source, marketplace string) (PluginDefinition, error) {
	manifest, err := LoadPluginFromDirectory(root)
	if err != nil {
		return nil, err
	}
	meta := PluginMetadata{
		ID: pluginID(manifest.Name, marketplace), Name: manifest.Name,
		Version: manifest.Version, Description: manifest.Description,
		Kind: kind, Source: source, DefaultEnabled: manifest.DefaultEnabled,
		Root: root,
	}
	hooks := resolveHooks(root, &manifest.Hooks)
	lifecycle := resolveLifecycle(root, &manifest.Lifecycle)
	tools := resolveTools(root, meta.ID, meta.Name, manifest.Tools)

	bp := basePlugin{metadata: meta, hooks: hooks, lifecycle: lifecycle, tools: tools}
	switch kind {
	case KindBuiltin:
		return &BuiltinPlugin{bp}, nil
	case KindBundled:
		return &BundledPlugin{bp}, nil
	default:
		return &ExternalPlugin{bp}, nil
	}
}

func pluginManifestPath(root string) (string, error) {
	direct := filepath.Join(root, manifestFileName)
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}
	packaged := filepath.Join(root, manifestRelativePath)
	if _, err := os.Stat(packaged); err == nil {
		return packaged, nil
	}
	return "", pluginErrNotFound(fmt.Sprintf(
		"plugin manifest not found at %s or %s", direct, packaged))
}

func loadManifestFromPath(root, manifestPath string) (*PluginManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, pluginErrNotFound(fmt.Sprintf("plugin manifest not found at %s: %v", manifestPath, err))
	}
	return buildPluginManifest(root, data)
}

func buildPluginManifest(root string, data []byte) (*PluginManifest, error) {
	var raw struct {
		Name          string                      `json:"name"`
		Version       string                      `json:"version"`
		Description   string                      `json:"description"`
		Permissions   []string                    `json:"permissions"`
		DefaultEnabled bool                        `json:"defaultEnabled"`
		Hooks         PluginHooks                 `json:"hooks"`
		Lifecycle     PluginLifecycle             `json:"lifecycle"`
		Tools         []rawToolManifest           `json:"tools"`
		Commands      []PluginCommandManifest     `json:"commands"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, pluginErrJSON(err)
	}

	var errors []ManifestValidationError
	validateRequiredField("name", raw.Name, &errors)
	validateRequiredField("version", raw.Version, &errors)
	validateRequiredField("description", raw.Description, &errors)

	perms := buildManifestPermissions(raw.Permissions, &errors)
	validateCommandEntries(root, raw.Hooks.PreToolUse, "hook", &errors)
	validateCommandEntries(root, raw.Hooks.PostToolUse, "hook", &errors)
	validateCommandEntries(root, raw.Lifecycle.Init, "lifecycle command", &errors)
	validateCommandEntries(root, raw.Lifecycle.Shutdown, "lifecycle command", &errors)
	tools := buildManifestTools(root, raw.Tools, &errors)
	commands := buildManifestCommands(root, raw.Commands, &errors)

	if len(errors) > 0 {
		var msgs []string
		for _, e := range errors {
			msgs = append(msgs, e.Error())
		}
		return nil, pluginErrValidation(strings.Join(msgs, "; "))
	}

	return &PluginManifest{
		Name: raw.Name, Version: raw.Version, Description: raw.Description,
		Permissions: perms, DefaultEnabled: raw.DefaultEnabled,
		Hooks: raw.Hooks, Lifecycle: raw.Lifecycle,
		Tools: tools, Commands: commands,
	}, nil
}

type rawToolManifest struct {
	Name               string      `json:"name"`
	Description        string      `json:"description"`
	InputSchema        interface{} `json:"inputSchema"`
	Command            string      `json:"command"`
	Args               []string    `json:"args"`
	RequiredPermission string      `json:"requiredPermission"`
}

func validateRequiredField(field, value string, errors *[]ManifestValidationError) {
	if strings.TrimSpace(value) == "" {
		*errors = append(*errors, ManifestValidationError{Type: "EmptyField", Field: field})
	}
}

func buildManifestPermissions(perms []string, errors *[]ManifestValidationError) []PluginPermission {
	seen := make(map[string]bool)
	var validated []PluginPermission
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "permission", Field: "value"})
			continue
		}
		if seen[p] {
			*errors = append(*errors, ManifestValidationError{Type: "DuplicatePermission", Field: p})
			continue
		}
		seen[p] = true
		if perm, ok := parsePluginPermission(p); ok {
			validated = append(validated, perm)
		} else {
			*errors = append(*errors, ManifestValidationError{Type: "InvalidPermission", Field: p})
		}
	}
	return validated
}

func buildManifestTools(root string, tools []rawToolManifest, errors *[]ManifestValidationError) []PluginToolManifest {
	seen := make(map[string]bool)
	var validated []PluginToolManifest
	for _, t := range tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "tool", Field: "name"})
			continue
		}
		if seen[name] {
			*errors = append(*errors, ManifestValidationError{Type: "DuplicateEntry", Kind: "tool", Name: name})
			continue
		}
		seen[name] = true
		if strings.TrimSpace(t.Description) == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "tool", Field: "description", Name: name})
		}
		if strings.TrimSpace(t.Command) == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "tool", Field: "command", Name: name})
		} else {
			validateCommandEntry(root, t.Command, "tool", errors)
		}
		if _, ok := t.InputSchema.(map[string]interface{}); !ok {
			*errors = append(*errors, ManifestValidationError{Type: "InvalidToolInputSchema", Name: name})
		}
		perm, ok := parsePluginToolPermission(strings.TrimSpace(t.RequiredPermission))
		if !ok {
			*errors = append(*errors, ManifestValidationError{
				Type: "InvalidToolRequiredPermission", Name: name, Field: t.RequiredPermission})
			continue
		}
		validated = append(validated, PluginToolManifest{
			Name: name, Description: t.Description, InputSchema: t.InputSchema,
			Command: t.Command, Args: t.Args, RequiredPermission: perm,
		})
	}
	return validated
}

func buildManifestCommands(root string, cmds []PluginCommandManifest, errors *[]ManifestValidationError) []PluginCommandManifest {
	seen := make(map[string]bool)
	var validated []PluginCommandManifest
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "command", Field: "name"})
			continue
		}
		if seen[name] {
			*errors = append(*errors, ManifestValidationError{Type: "DuplicateEntry", Kind: "command", Name: name})
			continue
		}
		seen[name] = true
		if strings.TrimSpace(c.Description) == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "command", Field: "description", Name: name})
		}
		if strings.TrimSpace(c.Command) == "" {
			*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: "command", Field: "command", Name: name})
		} else {
			validateCommandEntry(root, c.Command, "command", errors)
		}
		validated = append(validated, c)
	}
	return validated
}

func validateCommandEntries(root string, entries []string, kind string, errors *[]ManifestValidationError) {
	for _, entry := range entries {
		validateCommandEntry(root, entry, kind, errors)
	}
}

func validateCommandEntry(root, entry, kind string, errors *[]ManifestValidationError) {
	if strings.TrimSpace(entry) == "" {
		*errors = append(*errors, ManifestValidationError{Type: "EmptyEntryField", Kind: kind, Field: "command"})
		return
	}
	if isLiteralCommand(entry) {
		return
	}
	path := entry
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, entry)
	}
	if _, err := os.Stat(path); err != nil {
		*errors = append(*errors, ManifestValidationError{Type: "MissingPath", Kind: kind, Path: path})
	}
}

func resolveHooks(root string, hooks *PluginHooks) PluginHooks {
	return PluginHooks{
		PreToolUse:  resolveEntries(root, hooks.PreToolUse),
		PostToolUse: resolveEntries(root, hooks.PostToolUse),
	}
}

func resolveLifecycle(root string, lc *PluginLifecycle) PluginLifecycle {
	return PluginLifecycle{
		Init:     resolveEntries(root, lc.Init),
		Shutdown: resolveEntries(root, lc.Shutdown),
	}
}

func resolveTools(root, pluginID, pluginName string, tools []PluginToolManifest) []PluginTool {
	result := make([]PluginTool, len(tools))
	for i, t := range tools {
		result[i] = NewPluginTool(
			pluginID, pluginName,
			PluginToolDefinition{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema},
			resolveEntry(root, t.Command), t.Args, t.RequiredPermission, root,
		)
	}
	return result
}

func resolveEntries(root string, entries []string) []string {
	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = resolveEntry(root, e)
	}
	return result
}

func resolveEntry(root, entry string) string {
	if isLiteralCommand(entry) {
		return entry
	}
	return filepath.Join(root, entry)
}

func isLiteralCommand(entry string) bool {
	return !strings.HasPrefix(entry, "./") && !strings.HasPrefix(entry, "../") && !filepath.IsAbs(entry)
}

// isExistingFilePath checks whether the command looks like a file path
// (starts with "./" or "../" or is an absolute path) and the file exists.
// Used to decide between "sh <path>" (direct) vs "sh -lc <command>" (shell).
func isExistingFilePath(command string) bool {
	if !strings.HasPrefix(command, "./") && !strings.HasPrefix(command, "../") && !filepath.IsAbs(command) {
		return false
	}
	_, err := os.Stat(command)
	return err == nil
}

func validatePluginPaths(meta *PluginMetadata, hooks *PluginHooks, lc *PluginLifecycle, tools []PluginTool) error {
	if meta.Root == "" {
		return nil
	}
	for _, entry := range hooks.PreToolUse {
		if err := validateCommandPath(meta.Root, entry, "hook"); err != nil {
			return err
		}
	}
	for _, entry := range hooks.PostToolUse {
		if err := validateCommandPath(meta.Root, entry, "hook"); err != nil {
			return err
		}
	}
	for _, entry := range lc.Init {
		if err := validateCommandPath(meta.Root, entry, "lifecycle command"); err != nil {
			return err
		}
	}
	for _, entry := range lc.Shutdown {
		if err := validateCommandPath(meta.Root, entry, "lifecycle command"); err != nil {
			return err
		}
	}
	for _, tool := range tools {
		if err := validateCommandPath(meta.Root, tool.Command, "tool"); err != nil {
			return err
		}
	}
	return nil
}

func validateCommandPath(root, entry, kind string) error {
	if isLiteralCommand(entry) {
		return nil
	}
	path := entry
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, entry)
	}
	if _, err := os.Stat(path); err != nil {
		return pluginErrInvalid(fmt.Sprintf("%s path `%s` does not exist", kind, path))
	}
	return nil
}

func runLifecycleCommands(meta *PluginMetadata, phase string, commands []string) error {
	for _, command := range commands {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", command)
		} else {
			if isExistingFilePath(command) {
				cmd = exec.Command("sh", command)
			} else {
				cmd = exec.Command("sh", "-lc", command)
			}
		}
		if meta.Root != "" {
			cmd.Dir = meta.Root
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			stderr := strings.TrimSpace(string(out))
			if stderr == "" {
				return pluginErrCommand(fmt.Sprintf("plugin `%s` %s failed for `%s`: %v", meta.ID, phase, command, err))
			}
			return pluginErrCommand(fmt.Sprintf("plugin `%s` %s failed for `%s`: %s", meta.ID, phase, command, stderr))
		}
	}
	return nil
}

func resolveLocalSource(source string) (string, error) {
	if _, err := os.Stat(source); err == nil {
		return source, nil
	}
	return "", pluginErrNotFound(fmt.Sprintf("plugin source `%s` was not found", source))
}

func parseInstallSource(source string) (PluginInstallSource, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "git@") || strings.HasSuffix(strings.ToLower(source), ".git") {
		return PluginInstallSource{Type: "git_url", URL: source}, nil
	}
	path, err := resolveLocalSource(source)
	if err != nil {
		return PluginInstallSource{}, err
	}
	return PluginInstallSource{Type: "local_path", Path: path}, nil
}

func materializeSource(source *PluginInstallSource, tempRoot string) (string, error) {
	os.MkdirAll(tempRoot, 0755)
	switch source.Type {
	case "local_path":
		return source.Path, nil
	case "git_url":
		destination := filepath.Join(tempRoot, fmt.Sprintf("plugin-%d", unixTimeMs()))
		out, err := exec.Command("git", "clone", "--depth", "1", source.URL, destination).CombinedOutput()
		if err != nil {
			return "", pluginErrCommand(fmt.Sprintf("git clone failed for `%s`: %s", source.URL, strings.TrimSpace(string(out))))
		}
		return destination, nil
	default:
		return "", pluginErrInvalid(fmt.Sprintf("unknown source type: %s", source.Type))
	}
}

func discoverPluginDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, pluginErrIO(err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if _, err := pluginManifestPath(path); err == nil {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func pluginID(name, marketplace string) string {
	return fmt.Sprintf("%s@%s", name, marketplace)
}

func sanitizePluginID(pid string) string {
	var b strings.Builder
	for _, ch := range pid {
		switch ch {
		case '/', '\\', '@', ':':
			b.WriteRune('-')
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func describeInstallSource(source *PluginInstallSource) string {
	switch source.Type {
	case "local_path":
		return source.Path
	case "git_url":
		return source.URL
	default:
		return ""
	}
}

func defaultBundledRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "bundled")
}

func unixTimeMs() int64 {
	return time.Now().UnixMilli()
}

func copyDirAll(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func updateSettingsJSON(path string, update func(map[string]interface{})) {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := os.ReadFile(path)
	var root map[string]interface{}
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		root = make(map[string]interface{})
	} else {
		json.Unmarshal(data, &root)
	}
	if root == nil {
		root = make(map[string]interface{})
	}
	update(root)
	out, _ := json.MarshalIndent(root, "", "  ")
	os.WriteFile(path, out, 0644)
}

func boolPtr(b bool) *bool { return &b }
