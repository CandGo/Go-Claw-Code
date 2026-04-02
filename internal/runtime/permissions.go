package runtime

// PermissionMode controls what operations are allowed.
type PermissionMode int

const (
	PermReadOnly          PermissionMode = iota
	PermWorkspaceWrite     PermissionMode = iota + 1
	PermDangerFullAccess   PermissionMode = iota + 2
)

// PermissionPolicy enforces per-tool permission requirements.
type PermissionPolicy struct {
	DefaultMode PermissionMode
	ToolReqs   map[string]PermissionMode
}

// DefaultPermissionPolicy returns the standard policy.
func DefaultPermissionPolicy() PermissionPolicy {
	return PermissionPolicy{
		DefaultMode: PermWorkspaceWrite,
		ToolReqs: map[string]PermissionMode{
			"bash":           PermDangerFullAccess,
			"read_file":     PermReadOnly,
			"write_file":    PermWorkspaceWrite,
			"edit_file":     PermWorkspaceWrite,
			"glob_search":   PermReadOnly,
			"grep_search":   PermReadOnly,
			"web_fetch":     PermReadOnly,
			"web_search":    PermReadOnly,
			"agent":         PermDangerFullAccess,
			"notebook_edit": PermWorkspaceWrite,
			"todo_write":    PermWorkspaceWrite,
		},
	}
}

// Check returns the required permission mode for a tool.
func (p PermissionPolicy) Check(toolName string) PermissionMode {
	if req, ok := p.ToolReqs[toolName]; ok {
		return req
	}
	return p.DefaultMode
}
