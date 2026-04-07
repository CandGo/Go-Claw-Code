package tools

import (
	"fmt"
	"strings"
	"sync"
)

// =============================================================================
// Multi-Agent Team Workspace
// =============================================================================

// workspace tracks a named collaboration group for multi-agent coordination.
type workspace struct {
	label       string
	description string
	members     []string
}

var (
	workspaces = make(map[string]*workspace)
	wsMu       sync.Mutex
)

// teamCreateTool creates a named workspace for multi-agent collaboration.
func teamCreateTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TeamCreate",
		Permission:  PermWorkspaceWrite,
		Description: "Create a named workspace for multi-agent collaboration. Agents within the same workspace can coordinate via SendMessage.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"team_name": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier for the workspace",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional human-readable description of the workspace purpose",
				},
			},
			"required": []string{"team_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			name, _ := input["team_name"].(string)
			desc, _ := input["description"].(string)
			if name == "" {
				return "", fmt.Errorf("team_name is required")
			}

			wsMu.Lock()
			defer wsMu.Unlock()
			if _, exists := workspaces[name]; exists {
				return "", fmt.Errorf("workspace %q already exists", name)
			}
			workspaces[name] = &workspace{label: name, description: desc}
			return fmt.Sprintf("Workspace %q created", name), nil
		},
	}
}

// teamDeleteTool removes a workspace.
func teamDeleteTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TeamDelete",
		Permission:  PermDangerFullAccess,
		Description: "Remove a workspace and all its associated state.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"team_name": map[string]interface{}{
					"type":        "string",
					"description": "Identifier of the workspace to remove",
				},
			},
			"required": []string{"team_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			name, _ := input["team_name"].(string)
			wsMu.Lock()
			defer wsMu.Unlock()
			if _, exists := workspaces[name]; !exists {
				return "", fmt.Errorf("workspace %q not found", name)
			}
			delete(workspaces, name)
			return fmt.Sprintf("Workspace %q removed", name), nil
		},
	}
}

// sendMessageTool delivers a message to one teammate or broadcasts to all.
func sendMessageTool() *ToolSpec {
	return &ToolSpec{
		Name:        "SendMessage",
		Permission:  PermReadOnly,
		Description: "Deliver a message to an agent teammate. Use '*' as the recipient to broadcast to all members of the current workspace.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Recipient agent name, or '*' to broadcast to everyone",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message body (plain text)",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Short preview (5-10 words) for notification display",
				},
			},
			"required": []string{"to", "message"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			recipient, _ := input["to"].(string)
			body, _ := input["message"].(string)
			summary, _ := input["summary"].(string)

			if recipient == "" {
				return "", fmt.Errorf("recipient must not be empty")
			}

			var b strings.Builder
			if recipient == "*" {
				b.WriteString("Broadcast delivered")
			} else {
				fmt.Fprintf(&b, "Message delivered to %s", recipient)
			}
			if summary != "" {
				fmt.Fprintf(&b, " [%s]", summary)
			}
			fmt.Fprintf(&b, ": %s", body)
			return b.String(), nil
		},
	}
}
