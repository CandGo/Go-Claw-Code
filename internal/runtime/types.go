package runtime

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Error types ---

// RuntimeError represents an error in the conversation runtime.
// Mirrors Rust RuntimeError.
type RuntimeError struct {
	Kind    string // "stream", "tool", "validation", "permission", "context"
	Message string
	Cause   error
}

func (e *RuntimeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *RuntimeError) Unwrap() error {
	return e.Cause
}

// NewRuntimeError creates a new RuntimeError.
func NewRuntimeError(kind, message string, cause error) *RuntimeError {
	return &RuntimeError{Kind: kind, Message: message, Cause: cause}
}

// ToolError represents an error during tool execution.
// Mirrors Rust ToolError.
type ToolError struct {
	ToolName string
	Message  string
	Cause    error
}

func (e *ToolError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("tool %s: %v", e.ToolName, e.Cause)
	}
	return fmt.Sprintf("tool %s: %s", e.ToolName, e.Message)
}

func (e *ToolError) Unwrap() error {
	return e.Cause
}

// NewToolError creates a new ToolError.
func NewToolError(toolName, message string, cause error) *ToolError {
	return &ToolError{ToolName: toolName, Message: message, Cause: cause}
}

// --- AssistantEvent types ---

// AssistantEventType represents the type of a streaming event.
// Mirrors Rust AssistantEvent enum.
type AssistantEventType int

const (
	EventTextDelta AssistantEventType = iota
	EventToolUse
	EventUsage
	EventMessageStop
	EventThinking
)

// AssistantEvent represents a single streaming event from the API.
// Mirrors Rust AssistantEvent enum variants.
type AssistantEvent struct {
	Type     AssistantEventType
	Text     string // TextDelta
	ToolID   string // ToolUse
	ToolName string // ToolUse
	ToolInput map[string]interface{} // ToolUse
	Usage    *TokenUsage // Usage
}

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
	ToolName  string `json:"tool_name,omitempty"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

func (t *ToolResultBlock) ContentType() string { return "tool_result" }

// SystemReminderBlock is a runtime-injected reminder into the conversation.
// Used for plan mode notices, compaction summaries, and other mid-stream injections.
type SystemReminderBlock struct {
	Content string `json:"content"`
	Source  string `json:"source,omitempty"` // e.g. "compaction", "plan_mode", "hook"
}

func (s *SystemReminderBlock) ContentType() string { return "system_reminder" }

// ImageSource represents the source data for an image content block.
// Supports base64-encoded data and optional URL-based images.
type ImageSource struct {
	Type      string `json:"type"`                  // "base64"
	MediaType string `json:"media_type"`             // e.g. "image/png"
	Data      string `json:"data"`                   // base64 encoded
	URL       string `json:"url,omitempty"`           // or URL-based
}

// ImageBlock is an image content block, used to send images to the model.
type ImageBlock struct {
	Type   string      `json:"type"`
	Source ImageSource `json:"source"`
}

func (b *ImageBlock) ContentType() string { return "image" }

// mediaTypeFromExt returns the MIME media type for common image file extensions.
func mediaTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// NewImageBlockFromPath reads an image file from the given path, determines its
// media type from the file extension, base64-encodes the content, and returns
// an ImageBlock suitable for sending to the model.
func NewImageBlockFromPath(path string) (*ImageBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file %s: %w", path, err)
	}
	ext := filepath.Ext(path)
	mediaType := mediaTypeFromExt(ext)
	encoded := base64.StdEncoding.EncodeToString(data)
	return &ImageBlock{
		Type: "image",
		Source: ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      encoded,
		},
	}, nil
}

// MessageRole represents who sent a message.
type MessageRole string

const (
	MsgRoleUser      MessageRole = "user"
	MsgRoleAssistant MessageRole = "assistant"
	MsgRoleSystem    MessageRole = "system"
	MsgRoleTool      MessageRole = "tool"
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
	// Cache token tracking
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// Total returns the total token count.
func (t *TokenUsage) Total() int {
	return t.InputTokens + t.OutputTokens
}

// Add accumulates usage from another TokenUsage.
func (t *TokenUsage) Add(other *TokenUsage) {
	if other == nil {
		return
	}
	t.InputTokens += other.InputTokens
	t.OutputTokens += other.OutputTokens
	t.CacheCreationInputTokens += other.CacheCreationInputTokens
	t.CacheReadInputTokens += other.CacheReadInputTokens
}

// TurnOutput is a single piece of output from a conversation turn.
type TurnOutput struct {
	Type      string // "text", "text_delta", "tool_use", "tool_result", "thinking", "thinking_delta", "done"
	Text      string
	ToolName  string
	ToolID    string
	ToolInput map[string]interface{} // parsed tool call input for display
	IsError   bool
}

// TurnSummary records metadata about a completed conversation turn.
// Mirrors Rust TurnSummary.
type TurnSummary struct {
	ToolsCalled    []string    `json:"tools_called,omitempty"`
	TokenUsage     TokenUsage `json:"token_usage"`
	WasInterrupted bool      `json:"was_interrupted,omitempty"`
	TurnNumber     int       `json:"turn_number"`
	Duration       time.Duration `json:"-"`
	DurationMs     int64      `json:"duration_ms,omitempty"` // serialized as milliseconds
}
