package runtime

import (
	"encoding/json"
	"fmt"
	"os"
)

// Session holds the complete conversation state.
type Session struct {
	Version  int                  `json:"version"`
	Messages []ConversationMessage `json:"messages"`
	filePath string
}

// NewSession creates a new empty session.
func NewSession() *Session {
	return &Session{Version: 1}
}

// AddUserMessage appends a user message.
func (s *Session) AddUserMessage(blocks ...ContentBlock) {
	s.Messages = append(s.Messages, ConversationMessage{
		Role:    MsgRoleUser,
		Content: blocks,
	})
}

// AddAssistantMessage appends an assistant message.
func (s *Session) AddAssistantMessage(blocks []ContentBlock, usage *TokenUsage) {
	s.Messages = append(s.Messages, ConversationMessage{
		Role:    MsgRoleAssistant,
		Content: blocks,
		Usage:   usage,
	})
}

// Save persists the session to a JSON file.
func (s *Session) Save(path string) error {
	s.filePath = path

	var msgs []struct {
		Role    string     `json:"role"`
		Content []BlockDTO `json:"content"`
	}

	for _, m := range s.Messages {
		var dto []BlockDTO
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				dto = append(dto, BlockDTO{Type: "text", Text: v.Text})
			case *ToolUseBlock:
				dto = append(dto, BlockDTO{Type: "tool_use", ID: v.ID, Name: v.Name, Input: v.Input})
			case *ToolResultBlock:
				dto = append(dto, BlockDTO{Type: "tool_result", ToolUseID: v.ToolUseID, Content: v.Content, IsError: v.IsError})
			}
		}
		msgs = append(msgs, struct {
			Role    string     `json:"role"`
			Content []BlockDTO `json:"content"`
		}{Role: string(m.Role), Content: dto})
	}

	data := struct {
		Version  int `json:"version"`
		Messages []struct {
			Role    string     `json:"role"`
			Content []BlockDTO `json:"content"`
		} `json:"messages"`
	}{
		Version:  s.Version,
		Messages: msgs,
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

// LoadSession loads a session from a JSON file.
func LoadSession(path string) (*Session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	var data struct {
		Version int `json:"version"`
		Messages []struct {
			Role    string     `json:"role"`
			Content []BlockDTO `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	s := &Session{Version: data.Version, filePath: path}
	for _, msg := range data.Messages {
		var blocks []ContentBlock
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				blocks = append(blocks, &TextBlock{Text: b.Text})
			case "tool_use":
				blocks = append(blocks, &ToolUseBlock{ID: b.ID, Name: b.Name, Input: b.Input})
			case "tool_result":
				blocks = append(blocks, &ToolResultBlock{ToolUseID: b.ToolUseID, Content: fmt.Sprintf("%v", b.Content), IsError: b.IsError})
			}
		}
		s.Messages = append(s.Messages, ConversationMessage{
			Role:    MessageRole(msg.Role),
			Content: blocks,
		})
	}
	return s, nil
}

// BlockDTO is used for JSON serialization of content blocks.
type BlockDTO struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}
