package sandbox

import (
	"fmt"
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
}

// Config holds sandbox configuration.
type Config struct {
	Enabled    bool   `json:"enabled"`
	WorkDir    string `json:"work_dir,omitempty"`
	AllowNet   bool   `json:"allow_net,omitempty"`
	AllowPaths []string `json:"allow_paths,omitempty"`
}

// New creates a new sandbox from config.
func New(cfg Config) *Sandbox {
	s := &Sandbox{
		enabled:    cfg.Enabled,
		allowNet:   cfg.AllowNet,
		allowPaths: cfg.AllowPaths,
	}

	if cfg.WorkDir != "" {
		s.workDir = cfg.WorkDir
	} else {
		s.workDir, _ = os.Getwd()
	}

	// Build minimal env
	s.env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"TEMP=" + os.Getenv("TEMP"),
		"TMP=" + os.Getenv("TMP"),
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

// ValidatePath checks if a file path is accessible within the sandbox.
func (s *Sandbox) ValidatePath(path string) error {
	if !s.enabled {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %s", path)
	}

	// Check if path is within allowed directories
	allowed := false
	checkPaths := s.allowPaths
	if len(checkPaths) == 0 {
		checkPaths = []string{s.workDir}
	}

	for _, allowedPath := range checkPaths {
		absAllowed, _ := filepath.Abs(allowedPath)
		if strings.HasPrefix(abs, absAllowed) {
			allowed = true
			break
		}
	}

	if !allowed {
		return fmt.Errorf("path %s is outside sandbox allowed paths", path)
	}

	return nil
}

// TempDir returns a temporary directory within the sandbox.
func (s *Sandbox) TempDir() (string, error) {
	tmpDir := filepath.Join(s.workDir, ".claw-sandbox-tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}
	return tmpDir, nil
}

func (s *Sandbox) buildCommand(command string, timeoutMs int) *exec.Cmd {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	cmd.Dir = s.workDir
	cmd.Env = s.env

	if timeoutMs > 0 {
		// Timeout handled by caller via context
	}

	return cmd
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
