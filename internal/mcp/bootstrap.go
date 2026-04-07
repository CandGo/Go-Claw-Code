package mcp

import (
	"fmt"
)

// McpClientTransport mirrors Rust McpClientTransport (tagged union).
type McpClientTransport struct {
	Type       string // "stdio", "sse", "http", "websocket", "sdk", "managed_proxy"
	Stdio      *McpStdioTransport
	Remote     *McpRemoteTransport
	Sdk        *McpSdkTransport
	Proxy      *McpManagedProxyTransport
}

// McpStdioTransport mirrors Rust McpStdioTransport.
type McpStdioTransport struct {
	Command string
	Args    []string
	Env     map[string]string
}

// McpRemoteTransport mirrors Rust McpRemoteTransport.
type McpRemoteTransport struct {
	URL           string
	Headers       map[string]string
	HeadersHelper *string
	Auth          McpClientAuth
}

// McpSdkTransport mirrors Rust McpSdkTransport.
type McpSdkTransport struct {
	Name string
}

// McpManagedProxyTransport mirrors Rust McpManagedProxyTransport.
type McpManagedProxyTransport struct {
	URL string
	ID  string
}

// McpClientAuth mirrors Rust McpClientAuth.
type McpClientAuth struct {
	Type     string // "none" or "oauth"
	ClientID       *string
	CallbackPort   *int
	AuthServerURL  *string
}

// RequiresUserAuth returns true if the auth type requires user interaction.
func (a McpClientAuth) RequiresUserAuth() bool {
	return a.Type == "oauth"
}

// McpClientBootstrap mirrors Rust McpClientBootstrap.
type McpClientBootstrap struct {
	ServerName     string
	NormalizedName string
	ToolPrefix     string
	Signature      *string
	Transport      McpClientTransport
}

// NewMcpClientBootstrap mirrors Rust McpClientBootstrap::from_scoped_config.
func NewMcpClientBootstrap(serverName string, configType, command, url, id string, args []string, env, headers map[string]string, headersHelper *string) McpClientBootstrap {
	normalized := NormalizeNameForMCP(serverName)
	toolPrefix := MCPToolPrefix(serverName)
	sig := MCPServerSignature(configType, command, url, args)

	transport := McpClientTransportFromConfig(configType, command, url, id, args, env, headers, headersHelper)

	return McpClientBootstrap{
		ServerName:     serverName,
		NormalizedName: normalized,
		ToolPrefix:     toolPrefix,
		Signature:      sig,
		Transport:      transport,
	}
}

// McpClientTransportFromConfig mirrors Rust McpClientTransport::from_config.
func McpClientTransportFromConfig(configType, command, url, id string, args []string, env, headers map[string]string, headersHelper *string) McpClientTransport {
	switch configType {
	case "stdio":
		return McpClientTransport{
			Type: "stdio",
			Stdio: &McpStdioTransport{
				Command: command,
				Args:    args,
				Env:     env,
			},
		}
	case "sse":
		return McpClientTransport{
			Type: "sse",
			Remote: &McpRemoteTransport{
				URL:           url,
				Headers:       headers,
				HeadersHelper: headersHelper,
				Auth:          McpClientAuth{Type: "none"},
			},
		}
	case "http":
		return McpClientTransport{
			Type: "http",
			Remote: &McpRemoteTransport{
				URL:           url,
				Headers:       headers,
				HeadersHelper: headersHelper,
				Auth:          McpClientAuth{Type: "none"},
			},
		}
	case "websocket", "ws":
		return McpClientTransport{
			Type: "websocket",
			Remote: &McpRemoteTransport{
				URL:           url,
				Headers:       headers,
				HeadersHelper: headersHelper,
				Auth:          McpClientAuth{Type: "none"},
			},
		}
	case "sdk":
		return McpClientTransport{
			Type: "sdk",
			Sdk: &McpSdkTransport{
				Name: command, // command field holds name for SDK
			},
		}
	case "managed_proxy":
		return McpClientTransport{
			Type: "managed_proxy",
			Proxy: &McpManagedProxyTransport{
				URL: url,
				ID:  id,
			},
		}
	default:
		panic(fmt.Sprintf("unknown MCP config type: %s", configType))
	}
}
