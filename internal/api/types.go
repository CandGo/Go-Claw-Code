package api

import "encoding/json"

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ToolChoice controls tool use behavior.
type ToolChoice struct {
	Type     string `json:"type"`               // "auto", "any", "tool"
	ToolName string `json:"name,omitempty"`      // only when type="tool"
}

func AutoToolChoice() ToolChoice {
	return ToolChoice{Type: "auto"}
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MessageRequest is the request body sent to the API.
type MessageRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Messages    []InputMessage  `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  ToolChoice      `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream"`
}

// InputMessage is a single message in the conversation.
type InputMessage struct {
	Role    MessageRole       `json:"role"`
	Content []InputContentBlock `json:"content"`
}

// InputContentBlock is a union type for content blocks in input messages.
type InputContentBlock struct {
	Type string `json:"type"`

	// For text blocks
	Text string `json:"text,omitempty"`

	// For tool_use blocks (assistant)
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// For tool_result blocks (user)
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
	Content   []InputContentBlock    `json:"content,omitempty"` // nested content for tool_result
}

func TextBlock(text string) InputContentBlock {
	return InputContentBlock{Type: "text", Text: text}
}

func ToolUseBlock(id, name string, input map[string]interface{}) InputContentBlock {
	return InputContentBlock{Type: "tool_use", ID: id, Name: name, Input: input}
}

func ToolResultBlock(toolUseID string, content string, isError bool) InputContentBlock {
	return InputContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   []InputContentBlock{TextBlock(content)},
		IsError:   isError,
	}
}

// MessageResponse is the non-streaming response from the API.
type MessageResponse struct {
	ID      string               `json:"id"`
	Type    string               `json:"type"`
	Role    MessageRole          `json:"role"`
	Content []OutputContentBlock `json:"content"`
	Model   string               `json:"model"`
	Usage   Usage                `json:"usage"`
}

// OutputContentBlock is a union type for content blocks in responses.
type OutputContentBlock struct {
	Type string `json:"type"`

	// Text
	Text string `json:"text,omitempty"`

	// Tool use
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// Thinking
	Thinking string `json:"thinking,omitempty"`

	// Signature (for thinking blocks)
	Signature string `json:"signature,omitempty"`

	// Redacted thinking
	Data string `json:"data,omitempty"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// Cache token tracking for cost estimation
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// TotalInputTokens returns the effective total input tokens including cache reads.
func (u Usage) TotalInputTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// StreamEvent represents a single SSE event from the streaming API.
type StreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`

	// MessageStart
	Message *MessageStartData `json:"message,omitempty"`

	// ContentBlockStart
	ContentBlock *OutputContentBlock `json:"content_block,omitempty"`

	// ContentBlockDelta
	Delta *ContentBlockDelta `json:"delta,omitempty"`

	// MessageDelta
	UsageDelta *UsageDelta `json:"usage,omitempty"`

	// MessageStop — no data needed
}

type MessageStartData struct {
	ID    string      `json:"id"`
	Type  string      `json:"type"`
	Role  MessageRole `json:"role"`
	Model string      `json:"model"`
	Usage Usage       `json:"usage"`
}

type ContentBlockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

type UsageDelta struct {
	OutputTokens int `json:"output_tokens"`
}

// AssistantEvent is a simplified event used by the conversation runtime.
type AssistantEvent struct {
	Type string // "text", "tool_use", "thinking", "usage"

	Text      string
	ToolID    string
	ToolName  string
	ToolInput map[string]interface{}
	Thinking  string
	Usage     Usage
}

// NewTextEvent creates a text assistant event.
func NewTextEvent(text string) AssistantEvent {
	return AssistantEvent{Type: "text", Text: text}
}

// NewToolUseEvent creates a tool_use assistant event.
func NewToolUseEvent(id, name string, input map[string]interface{}) AssistantEvent {
	return AssistantEvent{Type: "tool_use", ToolID: id, ToolName: name, ToolInput: input}
}

// NewUsageEvent creates a usage assistant event.
func NewUsageEvent(u Usage) AssistantEvent {
	return AssistantEvent{Type: "usage", Usage: u}
}
