package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PluginKind identifies the type of plugin.
type PluginKind int

const (
	PluginBuiltin PluginKind = iota
	PluginBundled
	PluginExternal
)

// Plugin represents a loaded plugin.
type Plugin struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Kind        PluginKind             `json:"-"`
	Path        string                 `json:"path,omitempty"`

	// Manifest fields
	Command     string            `json:"command,omitempty"`    // Command to run for tool execution
	Tools       []PluginTool      `json:"tools,omitempty"`      // Tools provided by this plugin
	Hooks       PluginHooks       `json:"hooks,omitempty"`      // Hook configuration
	InitCommand string            `json:"init_command,omitempty"`
	ShutdownCommand string         `json:"shutdown_command,omitempty"`

	// State
	Enabled bool `json:"enabled"`
}

// PluginTool describes a tool provided by a plugin.
type PluginTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// PluginHooks holds hook commands for a plugin.
type PluginHooks struct {
	PreToolUse  []string `json:"pre_tool_use,omitempty"`
	PostToolUse []string `json:"post_tool_use,omitempty"`
}

// Manager manages plugin lifecycle.
type Manager struct {
	mu       sync.Mutex
	plugins  map[string]*Plugin
	homeDir  string
	cwdDir   string
}

// NewManager creates a new plugin manager.
func NewManager() *Manager {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return &Manager{
		plugins: make(map[string]*Plugin),
		homeDir: home,
		cwdDir:  cwd,
	}
}

// Discover finds all available plugins.
func (m *Manager) Discover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Search in project .claw/plugins/
	if err := m.scanDir(filepath.Join(m.cwdDir, ".claw", "plugins"), PluginExternal); err == nil {
	}

	// Search in user ~/.claw/plugins/
	if err := m.scanDir(filepath.Join(m.homeDir, ".claw", "plugins"), PluginExternal); err == nil {
	}

	return nil
}

func (m *Manager) scanDir(dir string, kind PluginKind) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, entry.Name(), "plugin.json")
		if _, err := os.Stat(manifestPath); err != nil {
			// Try alternate location
			manifestPath = filepath.Join(dir, entry.Name(), ".claw-plugin", "plugin.json")
			if _, err := os.Stat(manifestPath); err != nil {
				continue
			}
		}

		plugin, err := loadPlugin(manifestPath, kind)
		if err != nil {
			continue
		}
		plugin.Path = filepath.Dir(manifestPath)
		m.plugins[plugin.Name] = plugin
	}
	return nil
}

func loadPlugin(path string, kind PluginKind) (*Plugin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var plugin Plugin
	if err := json.Unmarshal(data, &plugin); err != nil {
		return nil, fmt.Errorf("failed to parse plugin %s: %w", path, err)
	}

	if plugin.Name == "" {
		return nil, fmt.Errorf("plugin missing name: %s", path)
	}

	plugin.Kind = kind
	plugin.Enabled = true
	return &plugin, nil
}

// List returns all discovered plugins.
func (m *Manager) List() []*Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*Plugin
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result
}

// Get returns a plugin by name.
func (m *Manager) Get(name string) *Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plugins[name]
}

// Enable enables a plugin.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}
	p.Enabled = true
	return nil
}

// Disable disables a plugin.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}
	p.Enabled = false
	return nil
}

// Install installs a plugin from a local path or git URL.
func (m *Manager) Install(source string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var pluginDir string
	if strings.HasPrefix(source, "http") || strings.HasPrefix(source, "git@") {
		// Git clone
		installDir := filepath.Join(m.homeDir, ".claw", "plugins", filepath.Base(source))
		installDir = strings.TrimSuffix(installDir, ".git")
		if err := os.MkdirAll(installDir, 0755); err != nil {
			return err
		}
		// In a full implementation, would run git clone here
		pluginDir = installDir
	} else {
		// Local path - create symlink or copy manifest
		pluginDir = filepath.Join(m.homeDir, ".claw", "plugins", filepath.Base(source))
		os.MkdirAll(pluginDir, 0755)
		manifestSrc := filepath.Join(source, "plugin.json")
		manifestDst := filepath.Join(pluginDir, "plugin.json")
		data, err := os.ReadFile(manifestSrc)
		if err != nil {
			return fmt.Errorf("no plugin.json found at %s: %w", source, err)
		}
		if err := os.WriteFile(manifestDst, data, 0644); err != nil {
			return err
		}
	}

	plugin, err := loadPlugin(filepath.Join(pluginDir, "plugin.json"), PluginExternal)
	if err != nil {
		return err
	}
	plugin.Path = pluginDir
	m.plugins[plugin.Name] = plugin

	return nil
}

// Uninstall removes a plugin.
func (m *Manager) Uninstall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if p.Path != "" {
		os.RemoveAll(p.Path)
	}
	delete(m.plugins, name)
	return nil
}

// AllTools returns tools from all enabled plugins.
func (m *Manager) AllTools() []PluginTool {
	m.mu.Lock()
	defer m.mu.Unlock()

	var tools []PluginTool
	for _, p := range m.plugins {
		if !p.Enabled {
			continue
		}
		tools = append(tools, p.Tools...)
	}
	return tools
}

// AllHooks returns aggregated hooks from all enabled plugins.
func (m *Manager) AllHooks() PluginHooks {
	m.mu.Lock()
	defer m.mu.Unlock()

	var hooks PluginHooks
	for _, p := range m.plugins {
		if !p.Enabled {
			continue
		}
		hooks.PreToolUse = append(hooks.PreToolUse, p.Hooks.PreToolUse...)
		hooks.PostToolUse = append(hooks.PostToolUse, p.Hooks.PostToolUse...)
	}
	return hooks
}
