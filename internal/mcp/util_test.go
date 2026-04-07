package mcp

import (
	"testing"
)

func TestNormalizeNameForMCP(t *testing.T) {
	if got := NormalizeNameForMCP("github.com"); got != "github_com" {
		t.Errorf("normalize(%q) = %q, want %q", "github.com", got, "github_com")
	}
	if got := NormalizeNameForMCP("tool name!"); got != "tool_name_" {
		t.Errorf("normalize(%q) = %q, want %q", "tool name!", got, "tool_name_")
	}
	if got := NormalizeNameForMCP("claude.ai Example   Server!!"); got != "claude_ai_Example_Server" {
		t.Errorf("normalize(%q) = %q, want %q", "claude.ai Example   Server!!", got, "claude_ai_Example_Server")
	}
}

func TestMCPToolName(t *testing.T) {
	got := MCPToolName("claude.ai Example Server", "weather tool")
	want := "mcp__claude_ai_Example_Server__weather_tool"
	if got != want {
		t.Errorf("MCPToolName = %q, want %q", got, want)
	}
}

func TestUnwrapCCRProxyURL(t *testing.T) {
	wrapped := "https://api.anthropic.com/v2/session_ingress/shttp/mcp/123?mcp_url=https%3A%2F%2Fvendor.example%2Fmcp&other=1"
	got := UnwrapCCRProxyURL(wrapped)
	if got != "https://vendor.example/mcp" {
		t.Errorf("unwrap = %q, want %q", got, "https://vendor.example/mcp")
	}

	plain := "https://vendor.example/mcp"
	if got := UnwrapCCRProxyURL(plain); got != plain {
		t.Errorf("plain URL should pass through, got %q", got)
	}
}

func TestMCPServerSignature(t *testing.T) {
	sig := MCPServerSignature("stdio", "uvx", "", []string{"mcp-server"})
	if sig == nil || *sig != "stdio:[uvx|mcp-server]" {
		t.Errorf("stdio sig = %v, want stdio:[uvx|mcp-server]", sig)
	}

	sig2 := MCPServerSignature("sse", "", "https://vendor.example/mcp", nil)
	if sig2 == nil || *sig2 != "url:https://vendor.example/mcp" {
		t.Errorf("sse sig = %v, want url:https://vendor.example/mcp", sig2)
	}

	sig3 := MCPServerSignature("sdk", "", "", nil)
	if sig3 != nil {
		t.Errorf("sdk sig should be nil, got %v", sig3)
	}
}

func TestScopedMCPConfigHashEqualForSameConfig(t *testing.T) {
	env := map[string]string{"TOKEN": "secret"}
	h1 := ScopedMCPConfigHash("stdio", "uvx", "", "", []string{"mcp-server"}, env, nil, "")
	h2 := ScopedMCPConfigHash("stdio", "uvx", "", "", []string{"mcp-server"}, env, nil, "")
	if h1 != h2 {
		t.Errorf("same config should have same hash: %s != %s", h1, h2)
	}
}

func TestScopedMCPConfigHashDiffForDiffConfig(t *testing.T) {
	h1 := ScopedMCPConfigHash("http", "", "https://vendor.example/mcp", "", nil, nil, nil, "")
	h2 := ScopedMCPConfigHash("http", "", "https://vendor.example/v2/mcp", "", nil, nil, nil, "")
	if h1 == h2 {
		t.Error("different configs should have different hashes")
	}
}

func TestBootstrapStdioTransport(t *testing.T) {
	bs := NewMcpClientBootstrap("stdio-server", "stdio", "uvx", "", "", []string{"mcp-server"}, map[string]string{"TOKEN": "secret"}, nil, nil)
	if bs.NormalizedName != "stdio-server" {
		t.Errorf("normalized = %q, want %q", bs.NormalizedName, "stdio-server")
	}
	if bs.ToolPrefix != "mcp__stdio-server__" {
		t.Errorf("prefix = %q, want %q", bs.ToolPrefix, "mcp__stdio-server__")
	}
	if bs.Transport.Type != "stdio" || bs.Transport.Stdio == nil {
		t.Error("expected stdio transport")
	}
	if bs.Transport.Stdio.Command != "uvx" {
		t.Errorf("command = %q, want %q", bs.Transport.Stdio.Command, "uvx")
	}
}

func TestBootstrapHTTPTransport(t *testing.T) {
	headers := map[string]string{"X-Test": "1"}
	bs := NewMcpClientBootstrap("remote server", "http", "", "https://vendor.example/mcp", "", nil, nil, headers, nil)
	if bs.NormalizedName != "remote_server" {
		t.Errorf("normalized = %q, want %q", bs.NormalizedName, "remote_server")
	}
	if bs.Transport.Type != "http" || bs.Transport.Remote == nil {
		t.Error("expected http transport")
	}
	if bs.Transport.Remote.URL != "https://vendor.example/mcp" {
		t.Errorf("url = %q", bs.Transport.Remote.URL)
	}
	if bs.Transport.Remote.Auth.RequiresUserAuth() {
		t.Error("non-oauth transport should not require user auth")
	}
}

func TestBootstrapSDKTransport(t *testing.T) {
	bs := NewMcpClientBootstrap("sdk server", "sdk", "sdk-server", "", "", nil, nil, nil, nil)
	if bs.Signature != nil {
		t.Error("sdk should have nil signature")
	}
	if bs.Transport.Type != "sdk" || bs.Transport.Sdk == nil {
		t.Error("expected sdk transport")
	}
	if bs.Transport.Sdk.Name != "sdk-server" {
		t.Errorf("sdk name = %q, want %q", bs.Transport.Sdk.Name, "sdk-server")
	}
}
