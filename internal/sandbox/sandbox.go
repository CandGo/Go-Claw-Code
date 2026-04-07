package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FilesystemIsolationMode controls filesystem sandboxing behavior.
type FilesystemIsolationMode int

const (
	IsolationOff          FilesystemIsolationMode = iota
	IsolationWorkspaceOnly
	IsolationAllowList
)

func (m FilesystemIsolationMode) String() string {
	switch m {
	case IsolationOff:
		return "off"
	case IsolationWorkspaceOnly:
		return "workspace-only"
	case IsolationAllowList:
		return "allow-list"
	default:
		return "unknown"
	}
}

// Sandbox is a backward-compatible facade around the functional sandbox API.
// It wraps SandboxConfig and SandboxStatus for callers that need a stateful object.
type Sandbox struct {
	config SandboxConfig
	status SandboxStatus
	cwd    string
}

// NewSandbox creates a new Sandbox from config.
func NewSandbox(cfg SandboxConfig) *Sandbox {
	cwd, _ := os.Getwd()
	s := &Sandbox{config: cfg, cwd: cwd}
	s.status = ResolveSandboxStatus(&cfg, cwd)
	return s
}

// New creates a new Sandbox from a Config struct (backward-compatible).
func New(cfg Config) *Sandbox {
	return NewSandbox(SandboxConfig{
		Enabled:              cfg.Enabled,
		NetworkIsolation:     !cfg.AllowNet,
		AllowedMounts:        cfg.AllowPaths,
	})
}

// Config holds sandbox configuration (backward-compatible).
type Config struct {
	Enabled         bool     `json:"enabled"`
	WorkDir         string   `json:"work_dir,omitempty"`
	AllowNet        bool     `json:"allow_net,omitempty"`
	AllowPaths      []string `json:"allow_paths,omitempty"`
	IsolationLevel  int      `json:"isolation_level,omitempty"`
}

// IsEnabled returns whether the sandbox is active.
func (s *Sandbox) IsEnabled() bool {
	return s.config.Enabled
}

// Config returns the sandbox configuration.
func (s *Sandbox) Config() SandboxConfig {
	return s.config
}

// ValidateCommand performs basic command safety checks.
func (s *Sandbox) ValidateCommand(command string) error {
	if !s.config.Enabled {
		return nil
	}
	dangerous := []string{
		"rm -rf /", "mkfs.", "dd if=", "> /dev/sd",
		":(){ :|:& };:", "chmod -R 777 /",
	}
	lower := strings.ToLower(command)
	for _, pattern := range dangerous {
		if strings.Contains(lower, pattern) {
			return fmt.Errorf("command blocked by sandbox: dangerous pattern detected")
		}
	}
	if s.NetworkIsolated() {
		netCmds := []string{"curl ", "wget ", "nc ", "ncat ", "ssh ", "scp ", "rsync "}
		for _, nc := range netCmds {
			if strings.Contains(lower, nc) {
				return fmt.Errorf("network command blocked: %s", strings.TrimSpace(nc))
			}
		}
	}
	return nil
}

// NetworkIsolated returns whether network access is blocked.
func (s *Sandbox) NetworkIsolated() bool {
	return s.config.Enabled && s.config.NetworkIsolation
}

// DefaultConfig returns a default sandbox config (disabled).
func DefaultConfig() Config {
	return Config{Enabled: false, AllowNet: false}
}

// SandboxConfig holds sandbox configuration with optional overrides.
type SandboxConfig struct {
	Enabled              bool                    `json:"enabled,omitempty"`
	NamespaceRestrictions bool                    `json:"namespace_restrictions,omitempty"`
	NetworkIsolation     bool                    `json:"network_isolation,omitempty"`
	FilesystemMode       FilesystemIsolationMode `json:"filesystem_mode,omitempty"`
	AllowedMounts        []string                `json:"allowed_mounts,omitempty"`
}

// SandboxRequest is a fully resolved sandbox request (no Optionals).
type SandboxRequest struct {
	Enabled              bool
	NamespaceRestrictions bool
	NetworkIsolation     bool
	FilesystemMode       FilesystemIsolationMode
	AllowedMounts        []string
}

// ContainerEnvironment holds container detection results.
type ContainerEnvironment struct {
	InContainer bool
	Markers     []string
}

// SandboxStatus is the full resolved sandbox state.
type SandboxStatus struct {
	Enabled            bool
	Requested          SandboxRequest
	Supported          bool
	Active             bool
	NamespaceSupported bool
	NamespaceActive    bool
	NetworkSupported   bool
	NetworkActive      bool
	FilesystemMode     FilesystemIsolationMode
	FilesystemActive   bool
	AllowedMounts      []string
	InContainer        bool
	ContainerMarkers   []string
	FallbackReason     string
}

// LinuxSandboxCommand is the resolved command for Linux namespace sandboxing.
type LinuxSandboxCommand struct {
	Program string
	Args    []string
	Env     [][2]string
}

// ResolveRequest resolves the config into a concrete request with optional overrides.
func (c SandboxConfig) ResolveRequest(
	enabledOverride *bool,
	namespaceOverride *bool,
	networkOverride *bool,
	fsModeOverride *FilesystemIsolationMode,
	mountsOverride *[]string,
) SandboxRequest {
	enabled := c.Enabled
	if enabledOverride != nil {
		enabled = *enabledOverride
	}
	ns := c.NamespaceRestrictions
	if namespaceOverride != nil {
		ns = *namespaceOverride
	}
	net := c.NetworkIsolation
	if networkOverride != nil {
		net = *networkOverride
	}
	fsMode := c.FilesystemMode
	if fsModeOverride != nil {
		fsMode = *fsModeOverride
	}
	mounts := c.AllowedMounts
	if mountsOverride != nil {
		mounts = *mountsOverride
	}
	return SandboxRequest{
		Enabled:              enabled,
		NamespaceRestrictions: ns,
		NetworkIsolation:     net,
		FilesystemMode:       fsMode,
		AllowedMounts:        mounts,
	}
}

// IsContainerized returns true if running inside a container.
func (s *Sandbox) IsContainerized() bool {
	return DetectContainerType() != ""
}

// DetectContainerType checks if running inside a container and returns
// a string identifying the container type. Mirrors Rust detect_container_environment.
// Returns an empty string if not in a container.
func DetectContainerType() string {
	// Check /.dockerenv
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	// Check /.dockerinit (Podman)
	if _, err := os.Stat("/.dockerinit"); err == nil {
		return "podman"
	}
	// Check /run/.containerenv
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return "containerenv"
	}
	// Check env vars
	for _, env := range []string{"container", "DOCKER_CONTAINER", "KUBERNETES_SERVICE_HOST"} {
		if os.Getenv(env) != "" {
			return "env:" + env
		}
	}
	// Check /proc/1/cgroup for container indicators
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		for _, indicator := range []string{"/docker/", "/containerd/", "/kubepods/"} {
			if strings.Contains(content, indicator) {
				return "cgroup:" + indicator
			}
		}
	}
	return ""
}

// DetectContainerEnvironment detects whether the current process is in a container.
func DetectContainerEnvironment() ContainerEnvironment {
	dockerenv := fileExists("/.dockerenv")
	containerenv := fileExists("/run/.containerenv")
	var procCgroup string
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		procCgroup = string(data)
	}
	return DetectContainerEnvironmentFrom(ContainerDetectionInputs{
		DockerenvExists:    dockerenv,
		ContainerenvExists: containerenv,
		Proc1Cgroup:        procCgroup,
	})
}

// ContainerDetectionInputs holds the inputs for container detection.
type ContainerDetectionInputs struct {
	DockerenvExists    bool
	ContainerenvExists bool
	Proc1Cgroup        string
}

// DetectContainerEnvironmentFrom detects container markers from given inputs.
func DetectContainerEnvironmentFrom(inputs ContainerDetectionInputs) ContainerEnvironment {
	var markers []string
	if inputs.DockerenvExists {
		markers = append(markers, "/.dockerenv")
	}
	if inputs.ContainerenvExists {
		markers = append(markers, "/run/.containerenv")
	}

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		normalized := strings.ToLower(key)
		if (normalized == "container" || normalized == "docker" || normalized == "podman" ||
			normalized == "kubernetes_service_host") && value != "" {
			markers = append(markers, fmt.Sprintf("env:%s=%s", key, value))
		}
	}

	if inputs.Proc1Cgroup != "" {
		for _, needle := range []string{"docker", "containerd", "kubepods", "podman", "libpod"} {
			if strings.Contains(inputs.Proc1Cgroup, needle) {
				markers = append(markers, fmt.Sprintf("/proc/1/cgroup:%s", needle))
			}
		}
	}

	sort.Strings(markers)
	// deduplicate
	seen := make(map[string]bool)
	unique := markers[:0]
	for _, m := range markers {
		if !seen[m] {
			seen[m] = true
			unique = append(unique, m)
		}
	}

	return ContainerEnvironment{
		InContainer: len(unique) > 0,
		Markers:     unique,
	}
}

// ResolveSandboxStatus resolves the sandbox status from config.
func ResolveSandboxStatus(config *SandboxConfig, cwd string) SandboxStatus {
	request := config.ResolveRequest(nil, nil, nil, nil, nil)
	return ResolveSandboxStatusForRequest(&request, cwd)
}

// ResolveSandboxStatusForRequest resolves sandbox status for a given request.
func ResolveSandboxStatusForRequest(request *SandboxRequest, cwd string) SandboxStatus {
	container := DetectContainerEnvironment()
	namespaceSupported := runtime.GOOS == "linux" && commandExists("unshare")
	networkSupported := namespaceSupported
	filesystemActive := request.Enabled && request.FilesystemMode != IsolationOff

	var fallbackReasons []string
	if request.Enabled && request.NamespaceRestrictions && !namespaceSupported {
		fallbackReasons = append(fallbackReasons, "namespace isolation unavailable (requires Linux with `unshare`)")
	}
	if request.Enabled && request.NetworkIsolation && !networkSupported {
		fallbackReasons = append(fallbackReasons, "network isolation unavailable (requires Linux with `unshare`)")
	}
	if request.Enabled && request.FilesystemMode == IsolationAllowList && len(request.AllowedMounts) == 0 {
		fallbackReasons = append(fallbackReasons, "filesystem allow-list requested without configured mounts")
	}

	active := request.Enabled &&
		(!request.NamespaceRestrictions || namespaceSupported) &&
		(!request.NetworkIsolation || networkSupported)

	allowedMounts := normalizeMounts(request.AllowedMounts, cwd)

	fallbackReason := ""
	if len(fallbackReasons) > 0 {
		fallbackReason = strings.Join(fallbackReasons, "; ")
	}

	return SandboxStatus{
		Enabled:            request.Enabled,
		Requested:          *request,
		Supported:          namespaceSupported,
		Active:             active,
		NamespaceSupported: namespaceSupported,
		NamespaceActive:    request.Enabled && request.NamespaceRestrictions && namespaceSupported,
		NetworkSupported:   networkSupported,
		NetworkActive:      request.Enabled && request.NetworkIsolation && networkSupported,
		FilesystemMode:     request.FilesystemMode,
		FilesystemActive:   filesystemActive,
		AllowedMounts:      allowedMounts,
		InContainer:        container.InContainer,
		ContainerMarkers:   container.Markers,
		FallbackReason:     fallbackReason,
	}
}

// BuildLinuxSandboxCommand builds the unshare command for Linux namespace sandboxing.
func BuildLinuxSandboxCommand(command string, cwd string, status *SandboxStatus) *LinuxSandboxCommand {
	if runtime.GOOS != "linux" || !status.Enabled ||
		(!status.NamespaceActive && !status.NetworkActive) {
		return nil
	}

	args := []string{
		"--user", "--map-root-user",
		"--mount", "--ipc", "--pid", "--uts", "--fork",
	}
	if status.NetworkActive {
		args = append(args, "--net")
	}
	args = append(args, "sh", "-lc", command)

	sandboxHome := filepath.Join(cwd, ".sandbox-home")
	sandboxTmp := filepath.Join(cwd, ".sandbox-tmp")
	env := [][2]string{
		{"HOME", sandboxHome},
		{"TMPDIR", sandboxTmp},
		{"CLAW_SANDBOX_FILESYSTEM_MODE", status.FilesystemMode.String()},
		{"CLAW_SANDBOX_ALLOWED_MOUNTS", strings.Join(status.AllowedMounts, ":")},
	}
	if pathEnv := os.Getenv("PATH"); pathEnv != "" {
		env = append(env, [2]string{"PATH", pathEnv})
	}

	return &LinuxSandboxCommand{
		Program: "unshare",
		Args:    args,
		Env:     env,
	}
}

// ValidatePath checks if a file path is accessible within the sandbox.
func (s *Sandbox) ValidatePath(path string) error {
	if !s.config.Enabled {
		return nil
	}
	allowed := s.config.AllowedMounts
	if len(allowed) == 0 {
		allowed = []string{s.cwd}
	}
	return ValidatePath(path, allowed)
}

// ValidatePath checks if a file path is accessible within the sandbox (standalone function).
func ValidatePath(path string, allowedPaths []string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %s", path)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		abs, _ = filepath.Abs(path)
	}
	clean := filepath.Clean(abs)
	if strings.Contains(clean, "..") {
		return fmt.Errorf("path traversal blocked: %s", path)
	}
	for _, allowed := range allowedPaths {
		absAllowed, _ := filepath.Abs(allowed)
		if strings.HasPrefix(clean, absAllowed) {
			return nil
		}
	}
	return fmt.Errorf("path %s is outside sandbox allowed paths", path)
}

// ExecuteInSandbox runs a command with sandbox isolation.
func ExecuteInSandbox(command string, cwd string, status *SandboxStatus) (string, error) {
	launcher := BuildLinuxSandboxCommand(command, cwd, status)
	if launcher != nil {
		cmd := exec.Command(launcher.Program, launcher.Args...)
		cmd.Dir = cwd
		env := make([]string, len(launcher.Env))
		for i, e := range launcher.Env {
			env[i] = e[0] + "=" + e[1]
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// Fallback: regular execution
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func normalizeMounts(mounts []string, cwd string) []string {
	result := make([]string, len(mounts))
	for i, mount := range mounts {
		p := filepath.FromSlash(mount)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		result[i] = p
	}
	return result
}

func commandExists(name string) bool {
	path, err := exec.LookPath(name)
	return err == nil && path != ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
