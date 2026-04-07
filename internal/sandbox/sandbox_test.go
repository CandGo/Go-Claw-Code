package sandbox

import (
	"strings"
	"testing"
)

func TestValidatePathWithinAllowed(t *testing.T) {
	err := ValidatePath("/workspace/src/main.go", []string{"/workspace"})
	if err != nil {
		t.Errorf("expected path within allowed dir to pass, got: %v", err)
	}
}

func TestValidatePathOutsideAllowed(t *testing.T) {
	err := ValidatePath("/etc/passwd", []string{"/workspace"})
	if err == nil {
		t.Error("expected path outside allowed dir to fail")
	}
}

func TestValidatePathTraversalBlocked(t *testing.T) {
	err := ValidatePath("/workspace/../../../etc/passwd", []string{"/workspace"})
	if err == nil {
		t.Error("expected path traversal to be blocked")
	}
}

func TestValidatePathEmptyAllowedFails(t *testing.T) {
	err := ValidatePath("/tmp/file.txt", []string{})
	if err == nil {
		t.Error("expected path to fail with empty allowed list")
	}
}

func TestValidatePathMultipleAllowedDirs(t *testing.T) {
	allowed := []string{"/workspace", "/tmp/claw"}
	if err := ValidatePath("/workspace/file.go", allowed); err != nil {
		t.Errorf("first allowed dir: %v", err)
	}
	if err := ValidatePath("/tmp/claw/output.txt", allowed); err != nil {
		t.Errorf("second allowed dir: %v", err)
	}
	if err := ValidatePath("/usr/bin/evil", allowed); err == nil {
		t.Error("non-allowed dir should fail")
	}
}

func TestSandboxValidateCommandDangerous(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: true},
		status: SandboxStatus{Enabled: true},
	}
	dangerous := []string{
		"rm -rf /",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		":(){ :|:& };:",
	}
	for _, cmd := range dangerous {
		if err := sb.ValidateCommand(cmd); err == nil {
			t.Errorf("expected dangerous command to be blocked: %s", cmd)
		}
	}
}

func TestSandboxValidateCommandSafe(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: true},
		status: SandboxStatus{Enabled: true},
	}
	safe := []string{
		"go build ./...",
		"ls -la",
		"git status",
		"echo hello",
	}
	for _, cmd := range safe {
		if err := sb.ValidateCommand(cmd); err != nil {
			t.Errorf("expected safe command to pass: %s, got: %v", cmd, err)
		}
	}
}

func TestSandboxValidateCommandNetworkBlocked(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: true, NetworkIsolation: true},
		status: SandboxStatus{Enabled: true, NetworkActive: true},
	}
	netCmds := []string{"curl http://example.com", "wget http://evil.com", "ssh user@host"}
	for _, cmd := range netCmds {
		if err := sb.ValidateCommand(cmd); err == nil {
			t.Errorf("expected network command to be blocked: %s", cmd)
		}
	}
}

func TestSandboxValidateCommandDisabled(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: false},
		status: SandboxStatus{Enabled: false},
	}
	// All commands should pass when sandbox is disabled
	if err := sb.ValidateCommand("rm -rf /"); err != nil {
		t.Errorf("disabled sandbox should not block commands: %v", err)
	}
}

func TestSandboxIsEnabled(t *testing.T) {
	sb := New(Config{Enabled: true})
	if !sb.IsEnabled() {
		t.Error("expected sandbox to be enabled")
	}
	sb2 := New(Config{Enabled: false})
	if sb2.IsEnabled() {
		t.Error("expected sandbox to be disabled")
	}
}

func TestSandboxNetworkIsolated(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: true, NetworkIsolation: true},
	}
	if !sb.NetworkIsolated() {
		t.Error("expected network isolation")
	}
	sb2 := &Sandbox{
		config: SandboxConfig{Enabled: true, NetworkIsolation: false},
	}
	if sb2.NetworkIsolated() {
		t.Error("expected no network isolation")
	}
}

func TestResolveRequestDefaults(t *testing.T) {
	cfg := SandboxConfig{
		Enabled:          true,
		NetworkIsolation: true,
		AllowedMounts:    []string{"/tmp"},
	}
	req := cfg.ResolveRequest(nil, nil, nil, nil, nil)
	if !req.Enabled {
		t.Error("expected enabled")
	}
	if !req.NetworkIsolation {
		t.Error("expected network isolation")
	}
	if len(req.AllowedMounts) != 1 || req.AllowedMounts[0] != "/tmp" {
		t.Errorf("AllowedMounts = %v, want [/tmp]", req.AllowedMounts)
	}
}

func TestResolveRequestOverrides(t *testing.T) {
	cfg := SandboxConfig{Enabled: false}
	enabled := true
	req := cfg.ResolveRequest(&enabled, nil, nil, nil, nil)
	if !req.Enabled {
		t.Error("override should enable")
	}
}

func TestDetectContainerEnvironmentFrom(t *testing.T) {
	ce := DetectContainerEnvironmentFrom(ContainerDetectionInputs{
		DockerenvExists: true,
	})
	if !ce.InContainer {
		t.Error("expected container detection with /.dockerenv")
	}
	found := false
	for _, m := range ce.Markers {
		if m == "/.dockerenv" {
			found = true
		}
	}
	if !found {
		t.Error("expected /.dockerenv in markers")
	}

	ce2 := DetectContainerEnvironmentFrom(ContainerDetectionInputs{})
	// May or may not detect container depending on env vars
	_ = ce2
}

func TestFilesystemIsolationModeString(t *testing.T) {
	tests := []struct {
		mode FilesystemIsolationMode
		want string
	}{
		{IsolationOff, "off"},
		{IsolationWorkspaceOnly, "workspace-only"},
		{IsolationAllowList, "allow-list"},
		{FilesystemIsolationMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("IsolationMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestSandboxValidatePathDisabled(t *testing.T) {
	sb := &Sandbox{
		config: SandboxConfig{Enabled: false},
	}
	// Should not validate when disabled
	if err := sb.ValidatePath("/any/path"); err != nil {
		t.Errorf("disabled sandbox should not validate paths: %v", err)
	}
}

func TestBuildLinuxSandboxCommandNonLinux(t *testing.T) {
	// On Windows, this should return nil regardless of status
	status := &SandboxStatus{
		Enabled:        true,
		NamespaceActive: true,
		NetworkActive:  true,
	}
	cmd := BuildLinuxSandboxCommand("echo test", "/tmp", status)
	// On Windows, this returns nil because runtime.GOOS != "linux"
	// We just verify it doesn't panic
	_ = cmd
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should have sandbox disabled")
	}
}

func TestNewFromConfig(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		AllowPaths: []string{"/workspace"},
		AllowNet:   false,
	}
	sb := New(cfg)
	if !sb.IsEnabled() {
		t.Error("expected enabled sandbox from config")
	}
}

func TestNormalizeMounts(t *testing.T) {
	mounts := normalizeMounts([]string{"src", "/abs/path"}, "/workspace")
	if !isAbs(mounts[0]) {
		t.Errorf("relative mount not resolved: %s", mounts[0])
	}
	// normalizeMounts uses filepath.FromSlash which converts / to \ on Windows
	// so just verify it contains the path segments
	if !strings.Contains(mounts[1], "abs") || !strings.Contains(mounts[1], "path") {
		t.Errorf("absolute mount should contain 'abs/path' segments: %s", mounts[1])
	}
}

func isAbs(p string) bool {
	return len(p) > 0 && (p[0] == '/' || p[0] == '\\')
}
