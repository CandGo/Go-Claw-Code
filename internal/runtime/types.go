package runtime

// ContentBlock represents a piece of content in a message.
type ContentBlock interface {
	ContentType() string
}

// TextBlock is a text content block.
type TextBlock struct {
	Text string `json:"text"`
}

func (t *TextBlock) ContentType() string { return "text" }

// ToolUseBlock represents a tool use request.
type ToolUseBlock struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

func (t *ToolUseBlock) ContentType() string { return "tool_use" }

// ToolResultBlock represents a tool execution result.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

func (t *ToolResultBlock) ContentType() string { return "tool_result" }

// MessageRole represents who sent a message.
type MessageRole string

const (
	MsgRoleUser      MessageRole = "user"
	MsgRoleAssistant MessageRole = "assistant"
)

// ConversationMessage is one message in the conversation.
type ConversationMessage struct {
	Role    MessageRole    `json:"role"`
	Content []ContentBlock `json:"-"`
	Usage   *TokenUsage    `json:"usage,omitempty"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TurnOutput is a single piece of output from a conversation turn.
type TurnOutput struct {
	Type     string // "text", "tool_use", "tool_result"
	Text     string
	ToolName string
	ToolID   string
	IsError  bool
}
