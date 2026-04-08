package web

// Envelope is the universal WebSocket message wrapper.
// Every message between client and server is a JSON object with a "type" field.
type Envelope struct {
	// Common
	Type string `json:"type"`
	ID   string `json:"id,omitempty"` // client-generated correlation ID

	// message (client → server)
	Content string `json:"content,omitempty"`

	// permission_response (client → server)
	PromptID string `json:"prompt_id,omitempty"`
	Decision string `json:"decision,omitempty"` // "allow" | "deny" | "allow_always"

	// command (client → server)
	Command string `json:"command,omitempty"`
	Args    string `json:"args,omitempty"`

	// text_delta / thinking_delta (server → client)
	Text string `json:"text,omitempty"`

	// tool_use / tool_result (server → client)
	ToolID    string                 `json:"tool_id,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`

	// permission_request (server → client)
	CurrentMode  string `json:"current_mode,omitempty"`
	RequiredMode string `json:"required_mode,omitempty"`

	// turn_done (server → client)
	Usage *UsageInfo `json:"usage,omitempty"`

	// error (server → client)
	Message string `json:"message,omitempty"`

	// connected (server → client)
	Model          string `json:"model,omitempty"`
	Version        string `json:"version,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`

	// session_info (server → client)
	MessageCount int    `json:"message_count,omitempty"`
	SessionID    string `json:"session_id,omitempty"`

	// session_list (server → client)
	Sessions []SessionEntry `json:"sessions,omitempty"`

	// command_output (server → client)
	Output string `json:"output,omitempty"`
}

// UsageInfo carries token consumption data.
type UsageInfo struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// SessionEntry is a saved session summary.
type SessionEntry struct {
	ID           string `json:"id"`
	MessageCount int    `json:"message_count"`
	Model        string `json:"model"`
	Modified     string `json:"modified"`
}
