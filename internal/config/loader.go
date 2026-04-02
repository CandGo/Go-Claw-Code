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

// MCPServer describes an MCP server connection with full transport support.
type MCPServer struct {
	Type    string            `json:"type"`              // stdio, sse, http, ws, sdk, managed-proxy
	Command string            `json:"command,omitempty"` // stdio: command to spawn
	Args    []string          `json:"args,omitempty"`    // stdio: command arguments
	URL     string            `json:"url,omitempty"`     // sse/http/ws: server URL
	Headers map[string]string `json:"headers,omitempty"` // sse/http/ws: auth headers
	Env     map[string]string `json:"env,omitempty"`     // stdio: environment variables
	Config  map[string]interface{} `json:"config,omitempty"` // transport-specific config
}

// PluginConfig holds plugin settings.
type PluginConfig struct {
	Enabled []string `json:"enabled,omitempty"`
}

// SandboxConfig holds sandbox settings.
type SandboxConfig struct {
	Enabled    bool     `json:"enabled,omitempty"`
	AllowNet   bool     `json:"allowNet,omitempty"`
	AllowPaths []string `json:"allowPaths,omitempty"`
	Isolation  int      `json:"isolation,omitempty"` // 1=path, 2=env, 3=namespace
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

// Load discovers and merges configuration from all layers using deep merge.
func Load() (*Config, error) {
	cfg := &Config{
		PermissionMode: "danger-full-access",
		MCPServers:     make(map[string]MCPServer),
		Settings:       make(map[string]interface{}),
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
		deepMergeInto(cfg, raw)
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
	if m := os.Getenv("CLAW_SANDBOX_ENABLED"); m == "true" || m == "1" {
		cfg.Sandbox.Enabled = true
	}
	if m := os.Getenv("CLAW_SANDBOX_NETWORK"); m == "true" || m == "1" {
		cfg.Sandbox.AllowNet = true
	}
	if m := os.Getenv("CLAW_SANDBOX_ISOLATION"); m != "" {
		switch m {
		case "path":
			cfg.Sandbox.Isolation = 1
		case "env":
			cfg.Sandbox.Isolation = 2
		case "namespace":
			cfg.Sandbox.Isolation = 3
		}
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

// deepMergeInto performs a deep merge of raw config into cfg.
// Nested maps are recursively merged; other values are overwritten.
func deepMergeInto(cfg *Config, raw map[string]interface{}) {
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

	// Deep merge hooks
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

	// Parse MCP servers with full transport support
	if servers, ok := raw["mcp_servers"].(map[string]interface{}); ok {
		for name, srv := range servers {
			if sm, ok := srv.(map[string]interface{}); ok {
				server := MCPServer{
					Type:    strValDefault(sm, "type", "stdio"),
					Command: strVal(sm, "command"),
					URL:     strVal(sm, "url"),
				}
				// Parse args
				if args, ok := sm["args"].([]interface{}); ok {
					for _, a := range args {
						if s, ok := a.(string); ok {
							server.Args = append(server.Args, s)
						}
					}
				}
				// Parse headers
				if headers, ok := sm["headers"].(map[string]interface{}); ok {
					server.Headers = make(map[string]string)
					for k, v := range headers {
						server.Headers[k] = fmt.Sprintf("%v", v)
					}
				}
				// Parse env
				if env, ok := sm["env"].(map[string]interface{}); ok {
					server.Env = make(map[string]string)
					for k, v := range env {
						server.Env[k] = fmt.Sprintf("%v", v)
					}
				}
				// Keep raw config for transport-specific options
				if cfg, ok := sm["config"].(map[string]interface{}); ok {
					server.Config = cfg
				}
				cfg.MCPServers[name] = server
			}
		}
	}

	// Parse plugins
	if plugins, ok := raw["plugins"].(map[string]interface{}); ok {
		if enabled, ok := plugins["enabled"].([]interface{}); ok {
			for _, v := range enabled {
				if s, ok := v.(string); ok {
					cfg.Plugins.Enabled = append(cfg.Plugins.Enabled, s)
				}
			}
		}
	}

	// Parse sandbox with full config
	if sb, ok := raw["sandbox"].(map[string]interface{}); ok {
		if v, ok := sb["enabled"].(bool); ok {
			cfg.Sandbox.Enabled = v
		}
		if v, ok := sb["allowNet"].(bool); ok {
			cfg.Sandbox.AllowNet = v
		}
		if v, ok := sb["allowPaths"].([]interface{}); ok {
			for _, p := range v {
				if s, ok := p.(string); ok {
					cfg.Sandbox.AllowPaths = append(cfg.Sandbox.AllowPaths, s)
				}
			}
		}
		if v, ok := sb["isolation"].(float64); ok {
			cfg.Sandbox.Isolation = int(v)
		}
	}

	// Deep merge settings
	if settings, ok := raw["settings"].(map[string]interface{}); ok {
		cfg.Settings = deepMergeMaps(cfg.Settings, settings)
	}
}

// deepMergeMaps recursively merges src into dst.
func deepMergeMaps(dst, src map[string]interface{}) map[string]interface{} {
	for k, v := range src {
		if srcMap, ok := v.(map[string]interface{}); ok {
			if dstMap, ok := dst[k].(map[string]interface{}); ok {
				dst[k] = deepMergeMaps(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func strValDefault(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
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
			fmt.Fprintf(&buf, "  [ok] %s: %s\n", name, layer.path)
		} else {
			fmt.Fprintf(&buf, "  [--] %s: (not found)\n", name)
		}
	}
	return buf.String()
}
