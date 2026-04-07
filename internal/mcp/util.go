package mcp

import (
	"fmt"
	"sort"
	"strings"
)

const claudeAIServerPrefix = "claude.ai "

var ccrProxyPathMarkers = []string{"/v2/session_ingress/shttp/mcp/", "/v2/ccr-sessions/"}

// NormalizeNameForMCP mirrors Rust normalize_name_for_mcp.
func NormalizeNameForMCP(name string) string {
	var b strings.Builder
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '-':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}
	normalized := b.String()

	if strings.HasPrefix(name, claudeAIServerPrefix) {
		normalized = collapseUnderscores(normalized)
		normalized = strings.Trim(normalized, "_")
	}

	return normalized
}

// MCPToolPrefix mirrors Rust mcp_tool_prefix.
func MCPToolPrefix(serverName string) string {
	return fmt.Sprintf("mcp__%s__", NormalizeNameForMCP(serverName))
}

// MCPToolName mirrors Rust mcp_tool_name.
func MCPToolName(serverName, toolName string) string {
	return MCPToolPrefix(serverName) + NormalizeNameForMCP(toolName)
}

// UnwrapCCRProxyURL mirrors Rust unwrap_ccr_proxy_url.
func UnwrapCCRProxyURL(url string) string {
	hasMarker := false
	for _, marker := range ccrProxyPathMarkers {
		if strings.Contains(url, marker) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return url
	}

	queryStart := strings.Index(url, "?")
	if queryStart < 0 {
		return url
	}
	query := url[queryStart+1:]
	for _, pair := range strings.Split(query, "&") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 && parts[0] == "mcp_url" {
			return percentDecode(parts[1])
		}
	}

	return url
}

// MCPServerSignature mirrors Rust mcp_server_signature.
// Returns nil for SDK configs.
func MCPServerSignature(configType, command, url string, args []string) *string {
	switch configType {
	case "stdio":
		cmd := append([]string{command}, args...)
		s := "stdio:" + renderCommandSignature(cmd)
		return &s
	case "sse", "http":
		s := "url:" + UnwrapCCRProxyURL(url)
		return &s
	case "websocket", "ws":
		s := "url:" + UnwrapCCRProxyURL(url)
		return &s
	case "managed_proxy":
		s := "url:" + UnwrapCCRProxyURL(url)
		return &s
	case "sdk":
		return nil
	}
	return nil
}

// ScopedMCPConfigHash mirrors Rust scoped_mcp_config_hash.
func ScopedMCPConfigHash(configType, command, url, id string, args []string, env, headers map[string]string, headersHelper string) string {
	var rendered string
	switch configType {
	case "stdio":
		rendered = fmt.Sprintf("stdio|%s|%s|%s", command, renderCommandSignature(args), renderEnvSignature(env))
	case "sse":
		rendered = fmt.Sprintf("sse|%s|%s|%s|", UnwrapCCRProxyURL(url), renderEnvSignature(headers), headersHelper)
	case "http":
		rendered = fmt.Sprintf("http|%s|%s|%s|", UnwrapCCRProxyURL(url), renderEnvSignature(headers), headersHelper)
	case "websocket", "ws":
		rendered = fmt.Sprintf("ws|%s|%s|%s", UnwrapCCRProxyURL(url), renderEnvSignature(headers), headersHelper)
	case "sdk":
		rendered = fmt.Sprintf("sdk|%s", command) // command holds name for sdk
	case "managed_proxy":
		rendered = fmt.Sprintf("claudeai-proxy|%s|%s", url, id)
	}
	return stableHexHash(rendered)
}

func renderCommandSignature(parts []string) string {
	escaped := make([]string, len(parts))
	for i, p := range parts {
		s := strings.ReplaceAll(p, "\\", "\\\\")
		s = strings.ReplaceAll(s, "|", "\\|")
		escaped[i] = s
	}
	return "[" + strings.Join(escaped, "|") + "]"
}

func renderEnvSignature(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	return strings.Join(parts, ";")
}

func stableHexHash(value string) string {
	var hash uint64 = 0xcbf29ce484222325
	for _, b := range []byte(value) {
		hash ^= uint64(b)
		hash *= 0x0100000001b3
	}
	return fmt.Sprintf("%016x", hash)
}

func collapseUnderscores(value string) string {
	var b strings.Builder
	lastWasUnderscore := false
	for _, ch := range value {
		if ch == '_' {
			if !lastWasUnderscore {
				b.WriteRune(ch)
			}
			lastWasUnderscore = true
		} else {
			b.WriteRune(ch)
			lastWasUnderscore = false
		}
	}
	return b.String()
}

func percentDecode(value string) string {
	bytes := []byte(value)
	var decoded []byte
	i := 0
	for i < len(bytes) {
		switch {
		case bytes[i] == '%' && i+2 < len(bytes):
			var hexByte byte
			_, err := fmt.Sscanf(string(bytes[i+1:i+3]), "%02x", &hexByte)
			if err == nil {
				decoded = append(decoded, hexByte)
				i += 3
				continue
			}
			decoded = append(decoded, bytes[i])
			i++
		case bytes[i] == '+':
			decoded = append(decoded, ' ')
			i++
		default:
			decoded = append(decoded, bytes[i])
			i++
		}
	}
	return string(decoded)
}
