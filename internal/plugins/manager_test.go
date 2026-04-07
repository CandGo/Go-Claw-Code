package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() map[string]interface{} {
	return map[string]interface{}{
		"name":        "test-plugin",
		"version":     "1.0.0",
		"description": "A test plugin",
	}
}

func writeManifest(t *testing.T, dir string, manifest map[string]interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func pluginDir(t *testing.T, tmpDir, name string) string {
	t.Helper()
	dir := filepath.Join(tmpDir, "plugins", "installed", name)
	os.MkdirAll(dir, 0755)
	return dir
}

func newTestManager(t *testing.T, tmpDir string) *PluginManager {
	t.Helper()
	cfg := PluginManagerConfig{
		ConfigHome:     tmpDir,
		InstallRoot:    filepath.Join(tmpDir, "plugins", "installed"),
		EnabledPlugins: make(map[string]bool),
	}
	return NewManager(cfg)
}

// --- Manifest loading tests ---

func TestLoadPluginFromDirectoryValid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, validManifest())

	manifest, err := LoadPluginFromDirectory(dir)
	if err != nil {
		t.Fatalf("LoadPluginFromDirectory failed: %v", err)
	}
	if manifest.Name != "test-plugin" {
		t.Errorf("Name = %q, want %q", manifest.Name, "test-plugin")
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", manifest.Version, "1.0.0")
	}
}

func TestLoadPluginMissingName(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	delete(manifest, "name")
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention name: %v", err)
	}
}

func TestLoadPluginMissingVersion(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	delete(manifest, "version")
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for missing version")
	}
}

func TestLoadPluginMissingDescription(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	delete(manifest, "description")
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for missing description")
	}
}

func TestLoadPluginWithTools(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	manifest["tools"] = []interface{}{
		map[string]interface{}{
			"name":               "my_tool",
			"description":        "Does something",
			"command":            "python",
			"inputSchema":        map[string]interface{}{"type": "object"},
			"requiredPermission": "read-only",
		},
	}
	writeManifest(t, dir, manifest)

	m, err := LoadPluginFromDirectory(dir)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if len(m.Tools) != 1 {
		t.Fatalf("Tools count = %d, want 1", len(m.Tools))
	}
	if m.Tools[0].Name != "my_tool" {
		t.Errorf("Tool name = %q", m.Tools[0].Name)
	}
}

func TestLoadPluginDuplicateTool(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	manifest["tools"] = []interface{}{
		map[string]interface{}{"name": "dup_tool", "description": "First", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
		map[string]interface{}{"name": "dup_tool", "description": "Second", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
	}
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for duplicate tool")
	}
}

func TestLoadPluginInvalidPermission(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	manifest["permissions"] = []interface{}{"invalid_perm"}
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for invalid permission")
	}
}

func TestLoadPluginValidPermissions(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	manifest["permissions"] = []interface{}{"read", "write", "execute"}
	writeManifest(t, dir, manifest)

	m, err := LoadPluginFromDirectory(dir)
	if err != nil {
		t.Fatalf("valid permissions should pass: %v", err)
	}
	if len(m.Permissions) != 3 {
		t.Errorf("Permissions count = %d, want 3", len(m.Permissions))
	}
}

func TestLoadPluginInvalidToolPermission(t *testing.T) {
	dir := t.TempDir()
	manifest := validManifest()
	manifest["tools"] = []interface{}{
		map[string]interface{}{
			"name": "tool1", "description": "A tool", "command": "echo",
			"inputSchema":        map[string]interface{}{"type": "object"},
			"requiredPermission": "invalid-level",
		},
	}
	writeManifest(t, dir, manifest)

	_, err := LoadPluginFromDirectory(dir)
	if err == nil {
		t.Fatal("expected validation error for invalid tool permission")
	}
}

// --- Helper function tests ---

func TestSanitizePluginID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-plugin", "my-plugin"},
		{"github.com/user/plugin", "github.com-user-plugin"},
		{"plugin@external", "plugin-external"},
		{"user@host:plugin.git", "user-host-plugin.git"},
	}
	for _, tt := range tests {
		result := sanitizePluginID(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizePluginID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsLiteralCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"./run.sh", false},
		{"../parent.sh", false},
		{"echo", true},
		{"python", true},
		{"node script.js", true},
	}
	for _, tt := range tests {
		result := isLiteralCommand(tt.input)
		if result != tt.want {
			t.Errorf("isLiteralCommand(%q) = %v, want %v", tt.input, result, tt.want)
		}
	}
}

func TestPluginKindString(t *testing.T) {
	tests := []struct {
		kind     PluginKind
		expected string
	}{
		{KindBuiltin, "builtin"},
		{KindBundled, "bundled"},
		{KindExternal, "external"},
		{PluginKind(99), "unknown"},
	}
	for _, tt := range tests {
		result := tt.kind.String()
		if result != tt.expected {
			t.Errorf("PluginKind(%d).String() = %q, want %q", tt.kind, result, tt.expected)
		}
	}
}

func TestManifestValidationErrorFormatting(t *testing.T) {
	tests := []struct {
		err  ManifestValidationError
		want string
	}{
		{
			ManifestValidationError{Type: "EmptyField", Field: "name"},
			"name",
		},
		{
			ManifestValidationError{Type: "InvalidPermission", Field: "bad"},
			"bad",
		},
		{
			ManifestValidationError{Type: "DuplicatePermission", Field: "read"},
			"read",
		},
	}
	for _, tt := range tests {
		s := tt.err.Error()
		if !strings.Contains(s, tt.want) {
			t.Errorf("Error() = %q, want to contain %q", s, tt.want)
		}
	}
}

func TestPluginLifecycleIsEmpty(t *testing.T) {
	if !(&PluginLifecycle{}).IsEmpty() {
		t.Error("empty lifecycle should be IsEmpty")
	}
	if (&PluginLifecycle{Init: []string{"echo"}}).IsEmpty() {
		t.Error("lifecycle with Init should not be IsEmpty")
	}
}

func TestPluginHooksIsEmpty(t *testing.T) {
	if !(&PluginHooks{}).IsEmpty() {
		t.Error("empty hooks should be IsEmpty")
	}
	if (&PluginHooks{PreToolUse: []string{"echo"}}).IsEmpty() {
		t.Error("hooks with PreToolUse should not be IsEmpty")
	}
}

func TestPluginHooksMergedWith(t *testing.T) {
	a := PluginHooks{PreToolUse: []string{"a_pre"}, PostToolUse: []string{"a_post"}}
	b := PluginHooks{PreToolUse: []string{"b_pre"}, PostToolUse: []string{"b_post"}}
	merged := a.MergedWith(&b)
	if len(merged.PreToolUse) != 2 {
		t.Errorf("PreToolUse count = %d, want 2", len(merged.PreToolUse))
	}
	if len(merged.PostToolUse) != 2 {
		t.Errorf("PostToolUse count = %d, want 2", len(merged.PostToolUse))
	}
}

// --- PluginManager tests ---

func TestManagerDiscoverPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	pDir := pluginDir(t, tmpDir, "test-plugin")
	writeManifest(t, pDir, validManifest())

	m := newTestManager(t, tmpDir)
	plugins, err := m.DiscoverPlugins()
	if err != nil {
		t.Fatalf("DiscoverPlugins failed: %v", err)
	}
	if len(plugins) == 0 {
		t.Fatal("DiscoverPlugins should find at least 1 plugin")
	}
	found := false
	for _, p := range plugins {
		if p.Metadata().Name == "test-plugin" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test-plugin not found after DiscoverPlugins")
	}
}

func TestManagerListPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	pDir := pluginDir(t, tmpDir, "test-plugin")
	writeManifest(t, pDir, validManifest())

	m := newTestManager(t, tmpDir)
	summaries, err := m.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins failed: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("ListPlugins should return at least 1 plugin")
	}
}

func TestManagerAggregatedTools(t *testing.T) {
	tmpDir := t.TempDir()
	p1Dir := pluginDir(t, tmpDir, "plugin-a")
	p2Dir := pluginDir(t, tmpDir, "plugin-b")

	m1 := validManifest()
	m1["tools"] = []interface{}{
		map[string]interface{}{"name": "tool_a", "description": "Tool A", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
	}
	writeManifest(t, p1Dir, m1)

	m2 := validManifest()
	m2["name"] = "plugin-b"
	m2["tools"] = []interface{}{
		map[string]interface{}{"name": "tool_b", "description": "Tool B", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
	}
	writeManifest(t, p2Dir, m2)

	mgr := newTestManager(t, tmpDir)
	// Enable plugins (external plugins default to disabled)
	reg, _ := mgr.PluginRegistry()
	for _, p := range reg.Plugins() {
		mgr.Enable(p.Metadata().ID)
	}

	tools, err := mgr.AggregatedTools()
	if err != nil {
		t.Fatalf("AggregatedTools failed: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("AggregatedTools count = %d, want 2", len(tools))
	}
}

func TestManagerAggregatedToolsDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	p1Dir := pluginDir(t, tmpDir, "plugin-a")
	p2Dir := pluginDir(t, tmpDir, "plugin-b")

	m1 := validManifest()
	m1["tools"] = []interface{}{
		map[string]interface{}{"name": "shared_tool", "description": "Tool from A", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
	}
	writeManifest(t, p1Dir, m1)

	m2 := validManifest()
	m2["name"] = "plugin-b"
	m2["defaultEnabled"] = true
	m2["tools"] = []interface{}{
		map[string]interface{}{"name": "shared_tool", "description": "Tool from B", "command": "echo", "inputSchema": map[string]interface{}{"type": "object"}, "requiredPermission": "read-only"},
	}
	writeManifest(t, p2Dir, m2)

	mgr := newTestManager(t, tmpDir)
	// Need to enable both plugins
	reg, _ := mgr.PluginRegistry()
	for _, p := range reg.Plugins() {
		mgr.Enable(p.Metadata().ID)
	}

	_, err := mgr.AggregatedTools()
	if err == nil {
		t.Fatal("expected deduplication error")
	}
	if !strings.Contains(err.Error(), "shared_tool") {
		t.Errorf("error should mention shared_tool: %v", err)
	}
}

func TestManagerAggregatedHooks(t *testing.T) {
	tmpDir := t.TempDir()
	pDir := pluginDir(t, tmpDir, "hook-plugin")
	manifest := validManifest()
	manifest["name"] = "hook-plugin"
	manifest["defaultEnabled"] = true
	manifest["hooks"] = map[string]interface{}{
		"PreToolUse":  []string{"echo pre"},
		"PostToolUse": []string{"echo post"},
	}
	writeManifest(t, pDir, manifest)

	m := newTestManager(t, tmpDir)
	// Enable the plugin
	reg, _ := m.PluginRegistry()
	for _, p := range reg.Plugins() {
		m.Enable(p.Metadata().ID)
	}

	hooks, err := m.AggregatedHooks()
	if err != nil {
		t.Fatalf("AggregatedHooks failed: %v", err)
	}
	if len(hooks.PreToolUse) != 1 || hooks.PreToolUse[0] != "echo pre" {
		t.Errorf("PreToolUse hooks = %v", hooks.PreToolUse)
	}
	if len(hooks.PostToolUse) != 1 || hooks.PostToolUse[0] != "echo post" {
		t.Errorf("PostToolUse hooks = %v", hooks.PostToolUse)
	}
}

func TestManagerEnableDisable(t *testing.T) {
	tmpDir := t.TempDir()
	pDir := pluginDir(t, tmpDir, "test-plugin")
	writeManifest(t, pDir, validManifest())

	m := newTestManager(t, tmpDir)
	reg, err := m.PluginRegistry()
	if err != nil {
		t.Fatalf("PluginRegistry failed: %v", err)
	}
	var pluginID string
	for _, p := range reg.Plugins() {
		pluginID = p.Metadata().ID
		break
	}
	if pluginID == "" {
		t.Fatal("no plugin found")
	}

	if err := m.Disable(pluginID); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}
	reg2, _ := m.PluginRegistry()
	p := reg2.Get(pluginID)
	if p == nil || p.IsEnabled() {
		t.Error("plugin should be disabled")
	}

	if err := m.Enable(pluginID); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	reg3, _ := m.PluginRegistry()
	p = reg3.Get(pluginID)
	if p == nil || !p.IsEnabled() {
		t.Error("plugin should be enabled")
	}
}

func TestManagerUninstallNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	m := newTestManager(t, tmpDir)
	err := m.Uninstall("nonexistent@external")
	if err == nil {
		t.Error("Uninstall(nonexistent) should error")
	}
}

func TestBuiltinPluginsExist(t *testing.T) {
	builtins := BuiltinPlugins()
	if len(builtins) == 0 {
		t.Error("BuiltinPlugins should return at least 1 plugin")
	}
	for _, p := range builtins {
		if p.Kind() != KindBuiltin {
			t.Errorf("BuiltinPlugin.Kind() = %v, want KindBuiltin", p.Kind())
		}
		if p.Metadata().Name == "" {
			t.Error("BuiltinPlugin should have a name")
		}
	}
}

func TestPluginRegistrySorted(t *testing.T) {
	plugins := []RegisteredPlugin{
		{Definition: &BuiltinPlugin{basePlugin{metadata: PluginMetadata{ID: "c@builtin"}}}, Enabled: true},
		{Definition: &BuiltinPlugin{basePlugin{metadata: PluginMetadata{ID: "a@builtin"}}}, Enabled: true},
		{Definition: &BuiltinPlugin{basePlugin{metadata: PluginMetadata{ID: "b@builtin"}}}, Enabled: true},
	}
	reg := NewPluginRegistry(plugins)
	ids := make([]string, len(reg.Plugins()))
	for i, p := range reg.Plugins() {
		ids[i] = p.Metadata().ID
	}
	if ids[0] != "a@builtin" || ids[1] != "b@builtin" || ids[2] != "c@builtin" {
		t.Errorf("not sorted: %v", ids)
	}
}

func TestPluginToolPermissionString(t *testing.T) {
	tests := []struct {
		perm PluginToolPermission
		want string
	}{
		{ToolPermReadOnly, "read-only"},
		{ToolPermWorkspaceWrite, "workspace-write"},
		{ToolPermDangerFullAccess, "danger-full-access"},
		{PluginToolPermission(99), "unknown"},
	}
	for _, tt := range tests {
		if tt.perm.String() != tt.want {
			t.Errorf("PluginToolPermission(%d).String() = %q, want %q", tt.perm, tt.perm.String(), tt.want)
		}
	}
}

func TestPluginErrorTypes(t *testing.T) {
	err := pluginErrValidation("test validation")
	if err.Error() != "test validation" {
		t.Errorf("validation error = %q", err.Error())
	}

	err2 := pluginErrNotFound("not found test")
	if err2.Error() != "not found test" {
		t.Errorf("not found error = %q", err2.Error())
	}
}
