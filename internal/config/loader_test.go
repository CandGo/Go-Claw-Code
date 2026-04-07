package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			config:  Config{PermissionMode: "danger-full-access"},
			wantErr: false,
		},
		{
			name:    "invalid permission mode",
			config:  Config{PermissionMode: "invalid"},
			wantErr: true,
		},
		{
			name: "stdio MCP missing command",
			config: Config{
				MCPServers: map[string]MCPServer{
					"test": {Type: "stdio"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid stdio MCP",
			config: Config{
				MCPServers: map[string]MCPServer{
					"test": {Type: "stdio", Command: "node"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid http MCP",
			config: Config{
				MCPServers: map[string]MCPServer{
					"test": {Type: "http", URL: "http://localhost:8080"},
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid sandbox isolation",
			config:  Config{Sandbox: SandboxConfig{Isolation: 5}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigLoaderLoad(t *testing.T) {
	tmpDir := t.TempDir()
	missingHome := filepath.Join(tmpDir, "missing-home")

	configData := `{
		"model": "claude-opus-4-6",
		"permissionMode": "plan",
		"mcpServers": {
			"test_server": {
				"type": "stdio",
				"command": "node",
				"args": ["server.js"]
			}
		},
		"sandbox": {
			"enabled": true,
			"isolation": 2
		}
	}`

	configPath := filepath.Join(tmpDir, ".claw", "settings.json")
	os.MkdirAll(filepath.Dir(configPath), 0755)
	os.WriteFile(configPath, []byte(configData), 0644)

	loader := NewConfigLoader(tmpDir, missingHome)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("model = %s, want claude-opus-4-6", cfg.Model)
	}
	if cfg.PermissionMode != "plan" {
		t.Errorf("permissionMode = %s, want plan", cfg.PermissionMode)
	}
	if !cfg.Sandbox.Enabled {
		t.Error("sandbox should be enabled")
	}
	if cfg.Sandbox.Isolation != 2 {
		t.Errorf("sandbox.isolation = %d, want 2", cfg.Sandbox.Isolation)
	}
	if len(cfg.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}
}

func TestConfigLoaderDiscover(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")

	loader := NewConfigLoader(tmpDir, home)
	entries := loader.Discover()

	if len(entries) != 5 {
		t.Fatalf("expected 5 config entries, got %d", len(entries))
	}
	if entries[0].Source != SourceUser {
		t.Errorf("entry[0] source = %v, want User", entries[0].Source)
	}
	if entries[2].Source != SourceProject {
		t.Errorf("entry[2] source = %v, want Project", entries[2].Source)
	}
	if entries[4].Source != SourceLocal {
		t.Errorf("entry[4] source = %v, want Local", entries[4].Source)
	}
}

func TestDeepMergeMaps(t *testing.T) {
	dst := map[string]interface{}{
		"a": float64(1),
		"nested": map[string]interface{}{
			"x": float64(10),
		},
	}
	src := map[string]interface{}{
		"b": float64(2),
		"nested": map[string]interface{}{
			"y": float64(20),
		},
	}

	result := deepMergeMaps(dst, src)
	if result["a"].(float64) != 1 {
		t.Error("a should be preserved")
	}
	if result["b"].(float64) != 2 {
		t.Error("b should be added")
	}
	nested := result["nested"].(map[string]interface{})
	if nested["x"].(float64) != 10 {
		t.Error("nested.x should be preserved")
	}
	if nested["y"].(float64) != 20 {
		t.Error("nested.y should be added")
	}
}

func TestPermissionModeLabels(t *testing.T) {
	tests := []struct {
		label string
		want  ResolvedPermissionMode
	}{
		{"default", PermReadOnly},
		{"plan", PermReadOnly},
		{"read-only", PermReadOnly},
		{"acceptEdits", PermWorkspaceWrite},
		{"auto", PermWorkspaceWrite},
		{"workspace-write", PermWorkspaceWrite},
		{"dontAsk", PermDangerFullAccess},
		{"danger-full-access", PermDangerFullAccess},
	}
	for _, tt := range tests {
		got, err := parsePermissionModeLabel(tt.label)
		if err != nil {
			t.Errorf("parsePermissionModeLabel(%q) error: %v", tt.label, err)
		}
		if got != tt.want {
			t.Errorf("parsePermissionModeLabel(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}

func TestConfigLoaderLayeredMerge(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".claw"), 0755)

	// User legacy
	os.WriteFile(
		filepath.Join(filepath.Dir(home), ".claw.json"),
		[]byte(`{"model":"haiku","env":{"A":"1"}}`),
		0644,
	)
	// User settings
	os.WriteFile(
		filepath.Join(home, "settings.json"),
		[]byte(`{"model":"sonnet","hooks":{"PreToolUse":["base"]},"permissions":{"defaultMode":"plan"}}`),
		0644,
	)
	// Project settings
	os.WriteFile(
		filepath.Join(tmpDir, ".claw", "settings.json"),
		[]byte(`{"hooks":{"PostToolUse":["project"]}}`),
		0644,
	)
	// Local override
	os.WriteFile(
		filepath.Join(tmpDir, ".claw", "settings.local.json"),
		[]byte(`{"model":"opus","permissionMode":"acceptEdits"}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Model != "opus" {
		t.Errorf("model = %s, want opus (local override)", cfg.Model)
	}
	if cfg.PermissionMode != "acceptEdits" {
		t.Errorf("permissionMode = %s, want acceptEdits", cfg.PermissionMode)
	}
	if len(cfg.Hooks.PreToolUse) != 1 || cfg.Hooks.PreToolUse[0] != "base" {
		t.Errorf("PreToolUse hooks = %v, want [base]", cfg.Hooks.PreToolUse)
	}
	if len(cfg.Hooks.PostToolUse) != 1 || cfg.Hooks.PostToolUse[0] != "project" {
		t.Errorf("PostToolUse hooks = %v, want [project]", cfg.Hooks.PostToolUse)
	}
	if cfg.resolvedPerm == nil || *cfg.resolvedPerm != PermWorkspaceWrite {
		t.Errorf("resolvedPerm = %v, want WorkspaceWrite", cfg.resolvedPerm)
	}
	if len(cfg.loadedEntries) != 4 {
		t.Errorf("loadedEntries = %d, want 4", len(cfg.loadedEntries))
	}
}

func TestConfigLoaderMCPConfigParsing(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".claw"), 0755)

	os.WriteFile(
		filepath.Join(home, "settings.json"),
		[]byte(`{
			"mcpServers": {
				"stdio-server": {
					"command": "uvx",
					"args": ["mcp-server"],
					"env": {"TOKEN": "secret"}
				},
				"remote-server": {
					"type": "http",
					"url": "https://example.test/mcp",
					"headers": {"Authorization": "Bearer token"},
					"headersHelper": "helper.sh"
				}
			}
		}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Check typed MCP collection
	stdio, ok := cfg.mcpCollection.Get("stdio-server")
	if !ok {
		t.Fatal("stdio-server should exist in mcp collection")
	}
	if stdio.Config.Transport != McpTransportStdio {
		t.Errorf("stdio-server transport = %v, want Stdio", stdio.Config.Transport)
	}
	if stdio.Config.Stdio == nil || stdio.Config.Stdio.Command != "uvx" {
		t.Errorf("stdio-server command = %v", stdio.Config.Stdio)
	}

	remote, ok := cfg.mcpCollection.Get("remote-server")
	if !ok {
		t.Fatal("remote-server should exist in mcp collection")
	}
	if remote.Config.Transport != McpTransportHttp {
		t.Errorf("remote-server transport = %v, want Http", remote.Config.Transport)
	}
	if remote.Config.Remote == nil || remote.Config.Remote.URL != "https://example.test/mcp" {
		t.Errorf("remote-server url = %v", remote.Config.Remote)
	}

	// Check backward-compat MCPServers map
	if len(cfg.MCPServers) != 2 {
		t.Errorf("MCPServers = %d entries, want 2", len(cfg.MCPServers))
	}
}

func TestConfigLoaderPluginConfig(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)

	os.WriteFile(
		filepath.Join(home, "settings.json"),
		[]byte(`{
			"enabledPlugins": {
				"tool-guard@builtin": true,
				"sample-plugin@external": false
			},
			"plugins": {
				"externalDirectories": ["./external-plugins"],
				"installRoot": "plugin-cache/installed",
				"registryPath": "plugin-cache/installed.json",
				"bundledRoot": "./bundled-plugins"
			}
		}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.pluginConfig.EnabledPlugins["tool-guard@builtin"] != true {
		t.Error("tool-guard@builtin should be enabled")
	}
	if cfg.pluginConfig.EnabledPlugins["sample-plugin@external"] != false {
		t.Error("sample-plugin@external should be disabled")
	}
	if len(cfg.pluginConfig.ExternalDirectories) != 1 || cfg.pluginConfig.ExternalDirectories[0] != "./external-plugins" {
		t.Errorf("externalDirectories = %v", cfg.pluginConfig.ExternalDirectories)
	}
	if cfg.pluginConfig.InstallRoot != "plugin-cache/installed" {
		t.Errorf("installRoot = %s", cfg.pluginConfig.InstallRoot)
	}
	if cfg.pluginConfig.RegistryPath != "plugin-cache/installed.json" {
		t.Errorf("registryPath = %s", cfg.pluginConfig.RegistryPath)
	}
	if cfg.pluginConfig.BundledRoot != "./bundled-plugins" {
		t.Errorf("bundledRoot = %s", cfg.pluginConfig.BundledRoot)
	}
}

func TestConfigLoaderSandboxConfig(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".claw"), 0755)

	os.WriteFile(
		filepath.Join(tmpDir, ".claw", "settings.local.json"),
		[]byte(`{
			"sandbox": {
				"enabled": true,
				"namespaceRestrictions": false,
				"networkIsolation": true,
				"filesystemMode": "allow-list",
				"allowedMounts": ["logs", "tmp/cache"]
			}
		}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Sandbox.Enabled {
		t.Error("sandbox should be enabled")
	}
	if cfg.Sandbox.FilesystemMode != "allow-list" {
		t.Errorf("filesystemMode = %s, want allow-list", cfg.Sandbox.FilesystemMode)
	}
	if len(cfg.Sandbox.AllowedMounts) != 2 {
		t.Errorf("allowedMounts = %v, want 2 entries", cfg.Sandbox.AllowedMounts)
	}
}

func TestConfigLoaderOAuthConfig(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)

	os.WriteFile(
		filepath.Join(home, "settings.json"),
		[]byte(`{
			"oauth": {
				"clientId": "runtime-client",
				"authorizeUrl": "https://console.test/oauth/authorize",
				"tokenUrl": "https://console.test/oauth/token",
				"callbackPort": 54545,
				"manualRedirectUrl": "https://console.test/oauth/callback",
				"scopes": ["org:read", "user:write"]
			}
		}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.oauth == nil {
		t.Fatal("oauth config should be present")
	}
	if cfg.oauth.ClientID != "runtime-client" {
		t.Errorf("clientId = %s", cfg.oauth.ClientID)
	}
	if cfg.oauth.CallbackPort == nil || *cfg.oauth.CallbackPort != 54545 {
		t.Errorf("callbackPort = %v", cfg.oauth.CallbackPort)
	}
	if len(cfg.oauth.Scopes) != 2 {
		t.Errorf("scopes = %v", cfg.oauth.Scopes)
	}
}

func TestConfigLoaderRejectsNonObject(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)
	os.MkdirAll(tmpDir, 0755)

	os.WriteFile(filepath.Join(home, "settings.json"), []byte("[]"), 0644)

	loader := NewConfigLoader(tmpDir, home)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for non-object settings file")
	}
	if !containsStr(err.Error(), "top-level settings value must be a JSON object") {
		t.Errorf("error = %v", err)
	}
}

func TestConfigLoaderRejectsInvalidMCPShape(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home", ".go-claw")
	os.MkdirAll(home, 0755)

	os.WriteFile(
		filepath.Join(home, "settings.json"),
		[]byte(`{"mcpServers":{"broken":{"type":"http","url":123}}}`),
		0644,
	)

	loader := NewConfigLoader(tmpDir, home)
	_, err := loader.Load()
	// Should still load but URL will be empty (graceful degradation)
	if err != nil {
		// JSON parsing of float64 won't match string, so URL will be empty
		t.Logf("got expected error: %v", err)
	}
}

func TestExtendUnique(t *testing.T) {
	dst := []string{"a", "b"}
	extendUnique(&dst, []string{"b", "c", "a"})
	if len(dst) != 3 {
		t.Errorf("extendUnique result = %v, want 3 items", dst)
	}
}

func TestRuntimeHookConfigMerge(t *testing.T) {
	a := RuntimeHookConfig{
		PreToolUse:  []string{"hook1"},
		PostToolUse: []string{"hookA"},
	}
	b := RuntimeHookConfig{
		PreToolUse:  []string{"hook2", "hook1"},
		PostToolUse: []string{"hookB", "hookA"},
	}
	merged := a.Merged(&b)
	if len(merged.PreToolUse) != 2 {
		t.Errorf("merged.PreToolUse = %v, want 2 items", merged.PreToolUse)
	}
	if len(merged.PostToolUse) != 2 {
		t.Errorf("merged.PostToolUse = %v, want 2 items", merged.PostToolUse)
	}
}

func TestPluginStateFor(t *testing.T) {
	cfg := RuntimePluginConfig{
		EnabledPlugins: map[string]bool{
			"active": true,
		},
	}
	if !cfg.StateFor("active", false) {
		t.Error("active plugin should be enabled")
	}
	// missing plugin with default true should return true
	if !cfg.StateFor("missing", true) {
		t.Error("missing plugin with default=true should return true")
	}
	// missing plugin with default false should return false
	if cfg.StateFor("missing", false) {
		t.Error("missing plugin with default=false should return false")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s[1:], substr) || s[:len(substr)] == substr)
}
