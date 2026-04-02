package runtime

import (
	"os"
	"strings"
)

// RemoteSessionContext holds information about a remote/forwarded session.
type RemoteSessionContext struct {
	IsRemote    bool
	IsContainer bool
	SessionType string // "local", "docker", "kubernetes", "ssh", "codespace"
}

// DetectRemoteContext probes the environment for remote/forwarded session indicators.
func DetectRemoteContext() RemoteSessionContext {
	ctx := RemoteSessionContext{SessionType: "local"}

	// Check for SSH
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		ctx.IsRemote = true
		ctx.SessionType = "ssh"
	}

	// Check for VS Code Remote / Codespaces
	if os.Getenv("VSCODE_IPC_HOOK_CLI") != "" || strings.Contains(os.Getenv("TERM_PROGRAM"), "vscode") {
		if os.Getenv("CODESPACES") != "" {
			ctx.IsRemote = true
			ctx.SessionType = "codespace"
		}
	}

	return ctx
}

// ProxyEnv returns proxy-related environment variables.
func ProxyEnv() map[string]string {
	envs := map[string]string{}
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
	} {
		if v := os.Getenv(key); v != "" {
			envs[key] = v
		}
	}
	return envs
}
