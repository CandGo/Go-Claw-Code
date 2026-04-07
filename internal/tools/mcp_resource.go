package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-claw/claw/internal/mcp"
)

// MCPResourceReader is the interface for reading MCP resources.
type MCPResourceReader interface {
	ListServerResources(ctx context.Context, serverName string) ([]mcp.Resource, error)
	ReadServerResource(ctx context.Context, serverName, uri string) (string, error)
}

// MCPPromptReader is the interface for reading MCP prompts.
type MCPPromptReader interface {
	ListServerPrompts(ctx context.Context, serverName string) ([]mcp.Prompt, error)
	GetServerPrompt(ctx context.Context, serverName, promptName string, args map[string]string) ([]mcp.PromptMessage, error)
}

var globalMCPResourceReader MCPResourceReader
var globalMCPPromptReader MCPPromptReader

// SetMCPResourceReader sets the global MCP resource reader.
func SetMCPResourceReader(r MCPResourceReader) {
	globalMCPResourceReader = r
}

// SetMCPPromptReader sets the global MCP prompt reader.
func SetMCPPromptReader(r MCPPromptReader) {
	globalMCPPromptReader = r
}

// mcpClientAdapter wraps *mcp.Client to implement MCPResourceReader and MCPPromptReader.
// Each Client is bound to a single server, so serverName is ignored.
type mcpClientAdapter struct {
	client *mcp.Client
}

// NewMCPClientAdapter creates an adapter wrapping a single MCP client.
func NewMCPClientAdapter(client *mcp.Client) *mcpClientAdapter {
	return &mcpClientAdapter{client: client}
}

func (a *mcpClientAdapter) ListServerResources(ctx context.Context, serverName string) ([]mcp.Resource, error) {
	return a.client.ListResources(ctx)
}

func (a *mcpClientAdapter) ReadServerResource(ctx context.Context, serverName, uri string) (string, error) {
	return a.client.ReadResource(ctx, uri)
}

func (a *mcpClientAdapter) ListServerPrompts(ctx context.Context, serverName string) ([]mcp.Prompt, error) {
	return a.client.ListPrompts(ctx)
}

func (a *mcpClientAdapter) GetServerPrompt(ctx context.Context, serverName, promptName string, args map[string]string) ([]mcp.PromptMessage, error) {
	return a.client.GetPrompt(ctx, promptName, args)
}

// multiMCPClientAdapter routes requests to the correct MCP client by server name.
// Implements both MCPResourceReader and MCPPromptReader for multi-server setups.
type multiMCPClientAdapter struct {
	clients map[string]*mcp.Client
}

// NewMultiMCPClientAdapter creates a multi-server adapter from a name→client map.
func NewMultiMCPClientAdapter(clients map[string]*mcp.Client) *multiMCPClientAdapter {
	return &multiMCPClientAdapter{clients: clients}
}

func (a *multiMCPClientAdapter) getClient(serverName string) (*mcp.Client, error) {
	c, ok := a.clients[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found (available: %v)", serverName, a.serverNames())
	}
	return c, nil
}

func (a *multiMCPClientAdapter) serverNames() []string {
	names := make([]string, 0, len(a.clients))
	for n := range a.clients {
		names = append(names, n)
	}
	return names
}

func (a *multiMCPClientAdapter) ListServerResources(ctx context.Context, serverName string) ([]mcp.Resource, error) {
	c, err := a.getClient(serverName)
	if err != nil {
		return nil, err
	}
	return c.ListResources(ctx)
}

func (a *multiMCPClientAdapter) ReadServerResource(ctx context.Context, serverName, uri string) (string, error) {
	c, err := a.getClient(serverName)
	if err != nil {
		return "", err
	}
	return c.ReadResource(ctx, uri)
}

func (a *multiMCPClientAdapter) ListServerPrompts(ctx context.Context, serverName string) ([]mcp.Prompt, error) {
	c, err := a.getClient(serverName)
	if err != nil {
		return nil, err
	}
	return c.ListPrompts(ctx)
}

func (a *multiMCPClientAdapter) GetServerPrompt(ctx context.Context, serverName, promptName string, args map[string]string) ([]mcp.PromptMessage, error) {
	c, err := a.getClient(serverName)
	if err != nil {
		return nil, err
	}
	return c.GetPrompt(ctx, promptName, args)
}

func mcpResourceTool() *ToolSpec {
	return &ToolSpec{
		Name:        "MCPReadResource",
		Permission:  PermReadOnly,
		Description: "Read a resource from an MCP server. Use this to access MCP server resources like documentation, data files, or configuration.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"server_name": map[string]interface{}{"type": "string", "description": "Name of the MCP server"},
				"uri":         map[string]interface{}{"type": "string", "description": "URI of the resource to read"},
			},
			"required": []string{"server_name", "uri"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			if globalMCPResourceReader == nil {
				return "", fmt.Errorf("MCP resource reading not available")
			}
			serverName, _ := input["server_name"].(string)
			uri, _ := input["uri"].(string)
			if serverName == "" || uri == "" {
				return "", fmt.Errorf("server_name and uri are required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return globalMCPResourceReader.ReadServerResource(ctx, serverName, uri)
		},
	}
}

func mcpListResourcesTool() *ToolSpec {
	return &ToolSpec{
		Name:        "MCPListResources",
		Permission:  PermReadOnly,
		Description: "List available resources from an MCP server.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"server_name": map[string]interface{}{"type": "string", "description": "Name of the MCP server"},
			},
			"required": []string{"server_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			if globalMCPResourceReader == nil {
				return "", fmt.Errorf("MCP resource listing not available")
			}
			serverName, _ := input["server_name"].(string)
			if serverName == "" {
				return "", fmt.Errorf("server_name is required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := globalMCPResourceReader.ListServerResources(ctx, serverName)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Sprintf("Resources from %s:\n%v", serverName, result), nil
			}
			return fmt.Sprintf("Resources from %s:\n%s", serverName, string(data)), nil
		},
	}
}

func mcpListPromptsTool() *ToolSpec {
	return &ToolSpec{
		Name:        "MCPListPrompts",
		Permission:  PermReadOnly,
		Description: "List available prompt templates from an MCP server.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"server_name": map[string]interface{}{"type": "string", "description": "Name of the MCP server"},
			},
			"required": []string{"server_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			if globalMCPPromptReader == nil {
				return "", fmt.Errorf("MCP prompt listing not available")
			}
			serverName, _ := input["server_name"].(string)
			if serverName == "" {
				return "", fmt.Errorf("server_name is required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := globalMCPPromptReader.ListServerPrompts(ctx, serverName)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Sprintf("Prompts from %s:\n%v", serverName, result), nil
			}
			return fmt.Sprintf("Prompts from %s:\n%s", serverName, string(data)), nil
		},
	}
}

func mcpGetPromptTool() *ToolSpec {
	return &ToolSpec{
		Name:        "MCPGetPrompt",
		Permission:  PermReadOnly,
		Description: "Get a prompt template from an MCP server. Returns the prompt messages that can be used to guide the model.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"server_name": map[string]interface{}{"type": "string", "description": "Name of the MCP server"},
				"prompt_name": map[string]interface{}{"type": "string", "description": "Name of the prompt template"},
				"arguments":   map[string]interface{}{"type": "object", "description": "Arguments for the prompt template", "additionalProperties": true},
			},
			"required": []string{"server_name", "prompt_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			if globalMCPPromptReader == nil {
				return "", fmt.Errorf("MCP prompt retrieval not available")
			}
			serverName, _ := input["server_name"].(string)
			promptName, _ := input["prompt_name"].(string)
			if serverName == "" || promptName == "" {
				return "", fmt.Errorf("server_name and prompt_name are required")
			}
			// Convert arguments to map[string]string
			args := make(map[string]string)
			if raw, ok := input["arguments"].(map[string]interface{}); ok {
				for k, v := range raw {
					if s, ok := v.(string); ok {
						args[k] = s
					} else {
						args[k] = fmt.Sprintf("%v", v)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := globalMCPPromptReader.GetServerPrompt(ctx, serverName, promptName, args)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Sprintf("Prompt from %s/%s:\n%v", serverName, promptName, result), nil
			}
			return fmt.Sprintf("Prompt from %s/%s:\n%s", serverName, promptName, string(data)), nil
		},
	}
}
