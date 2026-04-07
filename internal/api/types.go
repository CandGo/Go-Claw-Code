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

func AnyToolChoice() ToolChoice {
	return ToolChoice{Type: "any"}
}

func NamedToolChoice(name string) ToolChoice {
	return ToolChoice{Type: "tool", ToolName: name}
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ThinkingConfig configures extended thinking for the model.
type ThinkingConfig struct {
	Type         string `json:"type"`                         // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`       // token budget for thinking
}

// MessageRequest is the request body sent to the API.
// Mirrors Rust MessageRequest exactly.
type MessageRequest struct {
	Model      string          `json:"model"`
	MaxTokens  int             `json:"max_tokens"`
	Messages   []InputMessage  `json:"messages"`
	System     json.RawMessage `json:"system,omitempty"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice ToolChoice      `json:"tool_choice,omitempty"`
	Stream        bool            `json:"stream"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
}

// WithStreaming returns a copy of the request with streaming enabled.
func (r MessageRequest) WithStreaming() MessageRequest {
	r.Stream = true
	return r
}

// HasCacheControl returns true if any message or system content block has cache_control set.
func (r MessageRequest) HasCacheControl() bool {
	// Check system blocks (system is json.RawMessage with array of content blocks)
	if len(r.System) > 0 {
		var sysBlocks []struct {
			CacheControl *CacheControl `json:"cache_control"`
		}
		if json.Unmarshal(r.System, &sysBlocks) == nil {
			for _, b := range sysBlocks {
				if b.CacheControl != nil {
					return true
				}
			}
		}
	}
	// Check message content blocks
	for _, msg := range r.Messages {
		for _, block := range msg.Content {
			if block.CacheControl != nil {
				return true
			}
		}
	}
	return false
}

// InputMessage is a single message in the conversation.
type InputMessage struct {
	Role    MessageRole       `json:"role"`
	Content []InputContentBlock `json:"content"`
}

// UserTextMessage creates a user message with a text block.
func UserTextMessage(text string) InputMessage {
	return InputMessage{
		Role:    RoleUser,
		Content: []InputContentBlock{TextBlock(text)},
	}
}

// UserToolResultMessage creates a user message with a tool result block.
func UserToolResultMessage(toolUseID string, content string, isError bool) InputMessage {
	return InputMessage{
		Role: RoleUser,
		Content: []InputContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   []ToolResultContentBlock{{Type: "text", Text: content}},
				IsError:   isError,
			},
		},
	}
}

// InputContentBlock is a union type for content blocks in input messages.
// Mirrors Rust InputContentBlock.
type InputContentBlock struct {
	Type string `json:"type"`

	// For text blocks
	Text string `json:"text,omitempty"`

	// For tool_use blocks (assistant)
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// For tool_result blocks (user)
	ToolUseID string                    `json:"tool_use_id,omitempty"`
	IsError   bool                      `json:"is_error,omitempty"`
	Content   []ToolResultContentBlock  `json:"content,omitempty"` // nested content for tool_result

	// For image blocks (user)
	Source *ImageSource `json:"source,omitempty"`

	// For prompt caching
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource represents the source data for an image in the API request.
type ImageSource struct {
	Type      string `json:"type"`                   // "base64"
	MediaType string `json:"media_type"`              // e.g. "image/png"
	Data      string `json:"data"`                    // base64 encoded
}

// CacheControl represents the cache_control field on a content block.
// Used to enable prompt caching on specific content blocks.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// EphemeralCacheControl returns a CacheControl with type "ephemeral".
func EphemeralCacheControl() *CacheControl {
	return &CacheControl{Type: "ephemeral"}
}

// ToolResultContentBlock represents content within a tool result.
// Mirrors Rust ToolResultContentBlock.
type ToolResultContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	Value interface{} `json:"value,omitempty"` // for "json" type
}

func TextBlock(text string) InputContentBlock {
	return InputContentBlock{Type: "text", Text: text}
}

func ImageContentBlock(mediaType, data string) InputContentBlock {
	return InputContentBlock{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      data,
		},
	}
}

func ToolUseBlock(id, name string, input map[string]interface{}) InputContentBlock {
	return InputContentBlock{Type: "tool_use", ID: id, Name: name, Input: input}
}

func ToolResultBlock(toolUseID string, content string, isError bool) InputContentBlock {
	return InputContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   []ToolResultContentBlock{{Type: "text", Text: content}},
		IsError:   isError,
	}
}

// MessageResponse is the full response from the API.
// Mirrors Rust MessageResponse.
type MessageResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Role         MessageRole          `json:"role"`
	Content      []OutputContentBlock `json:"content"`
	Model        string               `json:"model"`
	StopReason   string               `json:"stop_reason,omitempty"`
	StopSequence string               `json:"stop_sequence,omitempty"`
	Usage        Usage                `json:"usage"`
	RequestID    string               `json:"request_id,omitempty"`
}

// TotalTokens returns input + output tokens.
func (r MessageResponse) TotalTokens() int {
	return r.Usage.TotalTokens()
}

// OutputContentBlock is a union type for content blocks in responses.
// Mirrors Rust OutputContentBlock.
type OutputContentBlock struct {
	Type string `json:"type"`

	// Text
	Text string `json:"text,omitempty"`

	// Tool use
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// Thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// Redacted thinking
	Data interface{} `json:"data,omitempty"`
}

// Usage tracks token consumption.
// Mirrors Rust Usage.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens"`
}

// TotalTokens returns input + output tokens.
func (u Usage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens
}

// TotalInputTokens returns the effective total input tokens including cache.
func (u Usage) TotalInputTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// ---- Streaming event types (mirrors Rust StreamEvent hierarchy) ----

// StreamEvent represents a single SSE event from the streaming API.
// Mirrors Rust StreamEvent.
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
	DeltaData  *MessageDeltaData `json:"delta_data,omitempty"`

	// MessageStop — no data needed
}

// MessageStartData holds the initial message data from a message_start event.
type MessageStartData struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Role    MessageRole `json:"role"`
	Model   string      `json:"model"`
	Usage   Usage       `json:"usage"`
}

// MessageDeltaData holds stop info from a message_delta event.
type MessageDeltaData struct {
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// ContentBlockDelta holds incremental content data.
// Mirrors Rust ContentBlockDelta.
type ContentBlockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// UsageDelta holds output token usage from message_delta.
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
	Signature string
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

// NewThinkingEvent creates a thinking assistant event.
func NewThinkingEvent(thinking string) AssistantEvent {
	return AssistantEvent{Type: "thinking", Thinking: thinking}
}

// NewSignatureEvent creates a signature assistant event.
func NewSignatureEvent(signature string) AssistantEvent {
	return AssistantEvent{Type: "thinking", Signature: signature}
}

// NewUsageEvent creates a usage assistant event.
func NewUsageEvent(u Usage) AssistantEvent {
	return AssistantEvent{Type: "usage", Usage: u}
}
