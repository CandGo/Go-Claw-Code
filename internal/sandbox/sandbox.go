package sandbox

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Sandbox provides isolated execution for tool commands.
type Sandbox struct {
	enabled    bool
	workDir    string
	allowNet   bool
	allowPaths []string
	env        []string
	// Isolation level controls how deep the sandbox goes.
	// 0 = disabled, 1 = path-only, 2 = env isolation, 3 = full namespace (Linux)
	isolationLevel int
}

// Config holds sandbox configuration.
type Config struct {
	Enabled    bool     `json:"enabled"`
	WorkDir    string   `json:"work_dir,omitempty"`
	AllowNet   bool     `json:"allow_net,omitempty"`
	AllowPaths []string `json:"allow_paths,omitempty"`
	// IsolationLevel: 1=path-only, 2=env-isolation, 3=namespace(Linux)
	IsolationLevel int `json:"isolation_level,omitempty"`
}

// New creates a new sandbox from config.
func New(cfg Config) *Sandbox {
	s := &Sandbox{
		enabled:        cfg.Enabled,
		allowNet:       cfg.AllowNet,
		allowPaths:     cfg.AllowPaths,
		isolationLevel: cfg.IsolationLevel,
	}

	if cfg.WorkDir != "" {
		s.workDir = cfg.WorkDir
	} else {
		s.workDir, _ = os.Getwd()
	}

	// Default isolation level
	if s.isolationLevel == 0 {
		s.isolationLevel = 1 // path-only by default
	}

	// Build minimal environment for isolation level >= 2
	if s.isolationLevel >= 2 {
		s.env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"USER=" + os.Getenv("USER"),
			"TEMP=" + os.Getenv("TEMP"),
			"TMP=" + os.Getenv("TMP"),
			"LANG=en_US.UTF-8",
			"TERM=dumb",
		}
		if runtime.GOOS == "windows" {
			s.env = append(s.env,
				"SystemRoot="+os.Getenv("SystemRoot"),
				"COMSPEC="+os.Getenv("COMSPEC"),
				"PATHEXT="+os.Getenv("PATHEXT"),
			)
		}
	}

	return s
}

// IsEnabled returns whether the sandbox is active.
func (s *Sandbox) IsEnabled() bool {
	return s.enabled
}

// Execute runs a command in the sandbox.
func (s *Sandbox) Execute(command string, timeoutMs int) (string, error) {
	if !s.enabled {
		return "", fmt.Errorf("sandbox not enabled")
	}

	cmd := s.buildCommand(command, timeoutMs)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ExecuteWithIsolation runs a command with the highest available isolation.
func (s *Sandbox) ExecuteWithIsolation(command string, timeoutMs int) (string, error) {
	if !s.enabled {
		return "", fmt.Errorf("sandbox not enabled")
	}

	// On Linux with level 3, use namespace isolation
	if runtime.GOOS == "linux" && s.isolationLevel >= 3 {
		return s.executeNamespaced(command, timeoutMs)
	}

	return s.Execute(command, timeoutMs)
}

// ValidatePath checks if a file path is accessible within the sandbox.
func (s *Sandbox) ValidatePath(path string) error {
	if !s.enabled {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %s", path)
	}

	// Resolve symlinks
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		// Path doesn't exist yet, check parent
		abs, _ = filepath.Abs(path)
	}

	// Block path traversal attempts
	clean := filepath.Clean(abs)
	if strings.Contains(clean, "..") {
		return fmt.Errorf("path traversal blocked: %s", path)
	}

	// Check if path is within allowed directories
	allowed := false
	checkPaths := s.allowPaths
	if len(checkPaths) == 0 {
		checkPaths = []string{s.workDir}
	}

	for _, allowedPath := range checkPaths {
		absAllowed, _ := filepath.Abs(allowedPath)
		if strings.HasPrefix(clean, absAllowed) {
			allowed = true
			break
		}
	}

	if !allowed {
		return fmt.Errorf("path %s is outside sandbox allowed paths", path)
	}

	return nil
}

// ValidateCommand performs basic command safety checks.
func (s *Sandbox) ValidateCommand(command string) error {
	if !s.enabled {
		return nil
	}

	// Block dangerous patterns
	dangerous := []string{
		"rm -rf /",
		"mkfs.",
		"dd if=",
		"> /dev/sd",
		":(){ :|:& };:",
		"chmod -R 777 /",
		"curl.*|.*sh",
		"wget.*|.*sh",
	}
	lower := strings.ToLower(command)
	for _, pattern := range dangerous {
		if strings.Contains(lower, pattern) {
			return fmt.Errorf("command blocked by sandbox: dangerous pattern detected")
		}
	}

	// If network is isolated, block network commands
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

// IsNetworkBlocked checks if a specific network address would be blocked.
func (s *Sandbox) IsNetworkBlocked(addr string) bool {
	if !s.NetworkIsolated() {
		return false
	}
	return true // All network is blocked when isolated
}

// TempDir returns a temporary directory within the sandbox.
func (s *Sandbox) TempDir() (string, error) {
	tmpDir := filepath.Join(s.workDir, ".claw-sandbox-tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}
	return tmpDir, nil
}

// AllowedHosts returns nil (all blocked) or the list of allowed hosts.
// Returns nil when network is not isolated.
func (s *Sandbox) AllowedHosts() []string {
	if !s.NetworkIsolated() {
		return nil // no restriction
	}
	return []string{} // empty = all blocked
}

// FreePort finds an available port (useful for sandbox networking).
func FreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (s *Sandbox) buildCommand(command string, timeoutMs int) *exec.Cmd {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	cmd.Dir = s.workDir

	// Use restricted environment for isolation level >= 2
	if s.isolationLevel >= 2 && len(s.env) > 0 {
		cmd.Env = s.env
	}

	return cmd
}

// executeNamespaced runs a command in Linux namespaces (requires unshare).
func (s *Sandbox) executeNamespaced(command string, timeoutMs int) (string, error) {
	// Build unshare command for namespace isolation
	args := []string{
		"--pid", "--mount", "--uts", "--ipc", "--fork",
		"--map-root-user",
		"sh", "-c", command,
	}

	if !s.allowNet {
		args = []string{"--net", "--pid", "--mount", "--uts", "--ipc", "--fork",
			"--map-root-user", "sh", "-c", command}
	}

	cmd := exec.Command("unshare", args...)
	cmd.Dir = s.workDir
	if len(s.env) > 0 {
		cmd.Env = s.env
	}

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// DefaultConfig returns a default sandbox config (disabled).
func DefaultConfig() Config {
	return Config{
		Enabled:  false,
		AllowNet: false,
	}
}

// NetworkIsolated returns whether network access is blocked.
func (s *Sandbox) NetworkIsolated() bool {
	return s.enabled && !s.allowNet
}

// IsolationLevel returns the current isolation level.
func (s *Sandbox) IsolationLevel() int {
	return s.isolationLevel
}

// IsContainer checks if the process is running inside a container.
func IsContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	for _, env := range []string{"CONTAINER", "DOCKER", "PODMAN"} {
		if os.Getenv(env) == "true" || os.Getenv(env) == "1" {
			return true
		}
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		for _, indicator := range []string{"docker", "containerd", "kubepods", "podman"} {
			if strings.Contains(content, indicator) {
				return true
			}
		}
	}
	return false
}
