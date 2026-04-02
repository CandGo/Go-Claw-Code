package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the merged application configuration.
type Config struct {
	Model          string                 `json:"model,omitempty"`
	PermissionMode string                 `json:"permissionMode,omitempty"`
	Hooks          HookConfig             `json:"hooks,omitempty"`
	MCPServers     map[string]MCPServer   `json:"mcp_servers,omitempty"`
	Plugins        PluginConfig           `json:"plugins,omitempty"`
	Sandbox        SandboxConfig          `json:"sandbox,omitempty"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
}

// HookConfig holds pre/post tool hook commands.
type HookConfig struct {
	PreToolUse  []string `json:"PreToolUse,omitempty"`
	PostToolUse []string `json:"PostToolUse,omitempty"`
}

// MCPServer describes an MCP server connection.
type MCPServer struct {
	Type    string                 `json:"type"` // stdio, sse, http, ws
	Command string                 `json:"command,omitempty"`
	URL     string                 `json:"url,omitempty"`
	Env     map[string]string      `json:"env,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// PluginConfig holds plugin settings.
type PluginConfig struct {
	Enabled []string `json:"enabled,omitempty"`
}

// SandboxConfig holds sandbox settings.
type SandboxConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// ConfigSource identifies where a config value came from.
type ConfigSource int

const (
	SourceUser ConfigSource = iota
	SourceUserSettings
	SourceProject
	SourceProjectSettings
	SourceLocal
)

// Load discovers and merges configuration from all layers.
func Load() (*Config, error) {
	cfg := &Config{
		PermissionMode: "danger-full-access",
		MCPServers:     make(map[string]MCPServer),
	}

	layers := discoverLayers()
	for _, layer := range layers {
		data, err := os.ReadFile(layer.path)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		mergeInto(cfg, raw)
	}

	// Apply environment variable overrides
	if m := os.Getenv("CLAW_MODEL"); m != "" {
		cfg.Model = m
	}
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		cfg.Model = m
	}
	if p := os.Getenv("CLAW_PERMISSION_MODE"); p != "" {
		cfg.PermissionMode = p
	}

	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}

	return cfg, nil
}

type configLayer struct {
	source ConfigSource
	path   string
}

func discoverLayers() []configLayer {
	home, _ := os.UserHomeDir()
	clawHome := os.Getenv("CLAW_CONFIG_HOME")
	if clawHome == "" {
		clawHome = filepath.Join(home, ".claw")
	}
	cwd, _ := os.Getwd()

	var layers []configLayer

	// 1. User legacy: $HOME/.claw.json
	layers = append(layers, configLayer{SourceUser, filepath.Join(home, ".claw.json")})

	// 2. User settings: $CLAW_CONFIG_HOME/settings.json
	layers = append(layers, configLayer{SourceUserSettings, filepath.Join(clawHome, "settings.json")})

	// 3. Project legacy: $CWD/.claw.json
	layers = append(layers, configLayer{SourceProject, filepath.Join(cwd, ".claw.json")})

	// 4. Project settings: $CWD/.claw/settings.json
	layers = append(layers, configLayer{SourceProjectSettings, filepath.Join(cwd, ".claw", "settings.json")})

	// 5. Local overrides: $CWD/.claw/settings.local.json
	layers = append(layers, configLayer{SourceLocal, filepath.Join(cwd, ".claw", "settings.local.json")})

	return layers
}

func mergeInto(cfg *Config, raw map[string]interface{}) {
	if v, ok := raw["model"].(string); ok && v != "" {
		cfg.Model = v
	}
	if v, ok := raw["permissionMode"].(string); ok {
		cfg.PermissionMode = v
	}
	// Also support permissions.defaultMode
	if perms, ok := raw["permissions"].(map[string]interface{}); ok {
		if dm, ok := perms["defaultMode"].(string); ok {
			cfg.PermissionMode = dm
		}
	}
	if hooks, ok := raw["hooks"].(map[string]interface{}); ok {
		if pre, ok := hooks["PreToolUse"].([]interface{}); ok {
			for _, v := range pre {
				if s, ok := v.(string); ok {
					cfg.Hooks.PreToolUse = append(cfg.Hooks.PreToolUse, s)
				}
			}
		}
		if post, ok := hooks["PostToolUse"].([]interface{}); ok {
			for _, v := range post {
				if s, ok := v.(string); ok {
					cfg.Hooks.PostToolUse = append(cfg.Hooks.PostToolUse, s)
				}
			}
		}
	}
	if servers, ok := raw["mcp_servers"].(map[string]interface{}); ok {
		for name, srv := range servers {
			if sm, ok := srv.(map[string]interface{}); ok {
				server := MCPServer{
					Type:   strVal(sm, "type"),
					Command: strVal(sm, "command"),
					URL:    strVal(sm, "url"),
				}
				if env, ok := sm["env"].(map[string]interface{}); ok {
					server.Env = make(map[string]string)
					for k, v := range env {
						server.Env[k] = fmt.Sprintf("%v", v)
					}
				}
				cfg.MCPServers[name] = server
			}
		}
	}
	if plugins, ok := raw["plugins"].(map[string]interface{}); ok {
		if enabled, ok := plugins["enabled"].([]interface{}); ok {
			for _, v := range enabled {
				if s, ok := v.(string); ok {
					cfg.Plugins.Enabled = append(cfg.Plugins.Enabled, s)
				}
			}
		}
	}
	if sandbox, ok := raw["sandbox"].(map[string]interface{}); ok {
		if v, ok := sandbox["enabled"].(bool); ok {
			cfg.Sandbox.Enabled = v
		}
	}
}

func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

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

// DescribeSources returns a human-readable list of config sources and their status.
func DescribeSources() string {
	layers := discoverLayers()
	var buf strings.Builder
	for _, layer := range layers {
		name := [...]string{"User (.claw.json)", "User settings", "Project (.claw.json)", "Project settings", "Local overrides"}[layer.source]
		if _, err := os.Stat(layer.path); err == nil {
			fmt.Fprintf(&buf, "  ✓ %s: %s\n", name, layer.path)
		} else {
			fmt.Fprintf(&buf, "  ✗ %s: (not found)\n", name)
		}
	}
	return buf.String()
}
