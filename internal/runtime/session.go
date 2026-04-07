package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// currentSessionVersion is the latest session format version.
const currentSessionVersion = 2

// SessionInfo holds lightweight metadata about a session without loading all messages.
type SessionInfo struct {
	Version      int        `json:"version"`
	Model        string     `json:"model,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	MessageCount int        `json:"message_count"`
	TotalTokens  int        `json:"total_tokens"`
}

// Session holds the complete conversation state.
// Mirrors Rust Session struct.
type Session struct {
	Version      int                   `json:"version"`
	Messages     []ConversationMessage `json:"messages"`
	Info         *SessionInfo          `json:"info,omitempty"`
	TurnSummaries []TurnSummary        `json:"turn_summaries,omitempty"`
	filePath     string
}

// SessionError represents an error during session operations.
type SessionError struct {
	Kind    string // "io", "json", "format"
	Message string
	Cause   error
}

func (e *SessionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *SessionError) Unwrap() error {
	return e.Cause
}

// TurnCount returns the number of completed turns.
func (s *Session) TurnCount() int {
	return len(s.TurnSummaries)
}

// AddTurnSummary records metadata about a completed turn.
func (s *Session) AddTurnSummary(summary TurnSummary) {
	s.TurnSummaries = append(s.TurnSummaries, summary)
}

// NewSession creates a new empty session.
func NewSession() *Session {	now := time.Now()
	return &Session{
		Version: currentSessionVersion,
		Info: &SessionInfo{
			Version:   currentSessionVersion,
			CreatedAt: &now,
		},
	}
}

// NewSessionEmpty creates an empty session with defaults (matches Rust Session::new()).
func NewSessionEmpty() *Session {
	return &Session{
		Version:  1,
		Messages: nil,
	}
}

// AddUserMessage appends a user text message.
func (s *Session) AddUserMessage(text string) {
	s.Messages = append(s.Messages, ConversationMessage{
		Role:    MsgRoleUser,
		Content: []ContentBlock{&TextBlock{Text: text}},
	})
}

// AddUserMessageBlocks appends a user message with explicit blocks.
func (s *Session) AddUserMessageBlocks(blocks ...ContentBlock) {
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

// AddSystemMessage appends a system message (used for compaction summaries).
func (s *Session) AddSystemMessage(blocks ...ContentBlock) {
	s.Messages = append(s.Messages, ConversationMessage{
		Role:    MsgRoleSystem,
		Content: blocks,
	})
}

// AddToolResult appends a tool result message.
func (s *Session) AddToolResult(toolUseID, toolName, output string, isError bool) {
	s.Messages = append(s.Messages, ConversationMessage{
		Role: MsgRoleTool,
		Content: []ContentBlock{&ToolResultBlock{
			ToolUseID: toolUseID,
			ToolName:  toolName,
			Content:   output,
			IsError:   isError,
		}},
	})
}

// SaveToPath persists the session to a JSON file.
// Mirrors Rust save_to_path.
func (s *Session) SaveToPath(path string) error {
	s.filePath = path
	// Update info before saving
	if s.Info == nil {
		s.Info = &SessionInfo{}
	}
	s.Info.Version = currentSessionVersion
	s.Info.MessageCount = len(s.Messages)
	s.Info.TotalTokens = s.totalTokens()

	type msgDTO struct {
		Role    string     `json:"role"`
		Content []BlockDTO `json:"content"`
		Usage   *TokenUsage `json:"usage,omitempty"`
	}

	var msgs []msgDTO
	for _, m := range s.Messages {
		var dto []BlockDTO
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				dto = append(dto, BlockDTO{Type: "text", Text: v.Text})
			case *ToolUseBlock:
				inputJSON := marshalBlockInput(v.Input)
				dto = append(dto, BlockDTO{Type: "tool_use", ID: v.ID, Name: v.Name, RawInput: inputJSON})
			case *ToolResultBlock:
				dto = append(dto, BlockDTO{
					Type:      "tool_result",
					ToolUseID: v.ToolUseID,
					ToolName:  v.ToolName,
					Content:   v.Content,
					IsError:   v.IsError,
				})
			case *SystemReminderBlock:
				dto = append(dto, BlockDTO{
					Type:    "system_reminder",
					Content: v.Content,
					Source:  v.Source,
				})
			case *ImageBlock:
				dto = append(dto, BlockDTO{
					Type: "image",
					ImageSource: &ImageSourceDTO{
						Type:      v.Source.Type,
						MediaType: v.Source.MediaType,
						Data:      v.Source.Data,
					},
				})
			}
		}
		msgs = append(msgs, msgDTO{
			Role:    string(m.Role),
			Content: dto,
			Usage:   m.Usage,
		})
	}

	data := struct {
		Version  int          `json:"version"`
		Info     *SessionInfo `json:"info,omitempty"`
		TurnSummaries []TurnSummary `json:"turn_summaries,omitempty"`
		Messages []msgDTO     `json:"messages"`
	}{
		Version:       s.Version,
		Info:          s.Info,
		TurnSummaries: s.TurnSummaries,
		Messages:      msgs,
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return &SessionError{Kind: "json", Message: "marshal session", Cause: err}
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
		return &SessionError{Kind: "io", Message: "write session", Cause: err}
	}
	return nil
}

// Save persists the session (alias for SaveToPath).
func (s *Session) Save(path string) error {
	return s.SaveToPath(path)
}

// LoadSession loads a session from a JSON file.
// Supports version migration from older formats.
func LoadSession(path string) (*Session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &SessionError{Kind: "io", Message: "failed to load session", Cause: err}
	}

	var data struct {
		Version       int `json:"version"`
		Info          *SessionInfo `json:"info"`
		TurnSummaries []TurnSummary `json:"turn_summaries,omitempty"`
		Messages []struct {
			Role    string     `json:"role"`
			Content []BlockDTO `json:"content"`
			Usage   *TokenUsage `json:"usage"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &SessionError{Kind: "json", Message: "failed to parse session", Cause: err}
	}

	s := &Session{
		Version:       data.Version,
		Info:          data.Info,
		TurnSummaries: data.TurnSummaries,
		filePath:      path,
	}

	// Version migration
	if s.Version < currentSessionVersion {
		s.migrate(s.Version, currentSessionVersion)
	}

	for _, msg := range data.Messages {
		var blocks []ContentBlock
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				blocks = append(blocks, &TextBlock{Text: b.Text})
			case "tool_use":
				input := unmarshalBlockInput(b.RawInput, b.Input)
				blocks = append(blocks, &ToolUseBlock{ID: b.ID, Name: b.Name, Input: input})
			case "tool_result":
				content := b.Content
				if content == nil {
					content = ""
				}
				blocks = append(blocks, &ToolResultBlock{
					ToolUseID: b.ToolUseID,
					ToolName:  b.ToolName,
					Content:   fmt.Sprintf("%v", content),
					IsError:   b.IsError,
				})
			case "system_reminder":
				blocks = append(blocks, &SystemReminderBlock{
					Content: fmt.Sprintf("%v", b.Content),
					Source:  b.Source,
				})
			case "image":
				if b.ImageSource != nil {
					blocks = append(blocks, &ImageBlock{
						Type: "image",
						Source: ImageSource{
							Type:      b.ImageSource.Type,
							MediaType: b.ImageSource.MediaType,
							Data:      b.ImageSource.Data,
						},
					})
				}
			}
		}
		s.Messages = append(s.Messages, ConversationMessage{
			Role:    MessageRole(msg.Role),
			Content: blocks,
			Usage:   msg.Usage,
		})
	}

	return s, nil
}

// LoadSessionInfo reads only the metadata from a session file without loading messages.
func LoadSessionInfo(path string) (*SessionInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &SessionError{Kind: "io", Message: "failed to read session info", Cause: err}
	}

	var data struct {
		Version  int          `json:"version"`
		Info     *SessionInfo `json:"info"`
		Messages []struct{}   `json:"messages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, &SessionError{Kind: "json", Message: "failed to parse session info", Cause: err}
	}

	info := data.Info
	if info == nil {
		info = &SessionInfo{Version: data.Version}
	}
	info.MessageCount = len(data.Messages)

	return info, nil
}

// ToJSON serializes the session using our custom JsonValue.
// Mirrors Rust Session::to_json.
func (s *Session) ToJSON() JsonValue {
	obj := make(map[string]JsonValue)
	obj["version"] = JsonNumberVal(int64(s.Version))
	msgs := make([]JsonValue, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = m.toJSONValue()
	}
	obj["messages"] = JsonArrayVal(msgs)
	return JsonObjectVal(obj)
}

// FromJSON deserializes a session from JsonValue.
// Mirrors Rust Session::from_json.
func SessionFromJSON(value JsonValue) (*Session, error) {
	obj := value.AsObject()
	if obj == nil {
		return nil, fmt.Errorf("session must be an object")
	}
	versionVal, ok := obj["version"]
	if !ok {
		return nil, fmt.Errorf("missing version")
	}
	version, ok := versionVal.AsInt64()
	if !ok {
		return nil, fmt.Errorf("version must be a number")
	}

	msgsVal, ok := obj["messages"]
	if !ok {
		return nil, fmt.Errorf("missing messages")
	}
	msgsArr := msgsVal.AsArray()
	if msgsArr == nil {
		return nil, fmt.Errorf("messages must be an array")
	}

	var messages []ConversationMessage
	for _, msgVal := range msgsArr {
		msg, err := conversationMessageFromJSON(msgVal)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *msg)
	}

	return &Session{
		Version:  int(version),
		Messages: messages,
	}, nil
}

func (m ConversationMessage) toJSONValue() JsonValue {
	obj := make(map[string]JsonValue)
	roleStr := "user"
	switch m.Role {
	case MsgRoleSystem:
		roleStr = "system"
	case MsgRoleUser:
		roleStr = "user"
	case MsgRoleAssistant:
		roleStr = "assistant"
	}
	if m.Role == MsgRoleTool {
		roleStr = "tool"
	}
	obj["role"] = JsonStringVal(roleStr)

	blocks := make([]JsonValue, len(m.Content))
	for i, b := range m.Content {
		blocks[i] = blockToJSONValue(b)
	}
	obj["blocks"] = JsonArrayVal(blocks)

	if m.Usage != nil {
		obj["usage"] = usageToJSONValue(*m.Usage)
	}
	return JsonObjectVal(obj)
}

func conversationMessageFromJSON(val JsonValue) (*ConversationMessage, error) {
	obj := val.AsObject()
	if obj == nil {
		return nil, fmt.Errorf("message must be an object")
	}

	roleVal, ok := obj["role"]
	if !ok {
		return nil, fmt.Errorf("missing role")
	}
	roleStr, ok := roleVal.AsString()
	if !ok {
		return nil, fmt.Errorf("role must be a string")
	}
	role := MsgRoleUser
	switch roleStr {
	case "system":
		role = MsgRoleSystem
	case "user":
		role = MsgRoleUser
	case "assistant":
		role = MsgRoleAssistant
	case "tool":
		role = MsgRoleTool
	default:
		return nil, fmt.Errorf("unsupported message role: %s", roleStr)
	}

	blocksVal, ok := obj["blocks"]
	if !ok {
		return nil, fmt.Errorf("missing blocks")
	}
	blocksArr := blocksVal.AsArray()
	if blocksArr == nil {
		return nil, fmt.Errorf("blocks must be an array")
	}
	var blocks []ContentBlock
	for _, bv := range blocksArr {
		b, err := blockFromJSONValue(bv)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}

	var usage *TokenUsage
	if usageVal, ok := obj["usage"]; ok {
		u, err := usageFromJSONValue(usageVal)
		if err != nil {
			return nil, err
		}
		usage = &u
	}

	return &ConversationMessage{Role: role, Content: blocks, Usage: usage}, nil
}

func blockToJSONValue(b ContentBlock) JsonValue {
	obj := make(map[string]JsonValue)
	switch v := b.(type) {
	case *TextBlock:
		obj["type"] = JsonStringVal("text")
		obj["text"] = JsonStringVal(v.Text)
	case *ToolUseBlock:
		obj["type"] = JsonStringVal("tool_use")
		obj["id"] = JsonStringVal(v.ID)
		obj["name"] = JsonStringVal(v.Name)
		inputJSON := marshalBlockInput(v.Input)
		obj["input"] = JsonStringVal(inputJSON)
	case *ToolResultBlock:
		obj["type"] = JsonStringVal("tool_result")
		obj["tool_use_id"] = JsonStringVal(v.ToolUseID)
		obj["tool_name"] = JsonStringVal(v.ToolName)
		obj["output"] = JsonStringVal(v.Content)
		obj["is_error"] = JsonBoolVal(v.IsError)
	case *SystemReminderBlock:
		obj["type"] = JsonStringVal("system_reminder")
		obj["content"] = JsonStringVal(v.Content)
		if v.Source != "" {
			obj["source"] = JsonStringVal(v.Source)
		}
	case *ImageBlock:
		obj["type"] = JsonStringVal("image")
		srcObj := make(map[string]JsonValue)
		srcObj["type"] = JsonStringVal(v.Source.Type)
		srcObj["media_type"] = JsonStringVal(v.Source.MediaType)
		srcObj["data"] = JsonStringVal(v.Source.Data)
		obj["source"] = JsonObjectVal(srcObj)
	}
	return JsonObjectVal(obj)
}

func blockFromJSONValue(val JsonValue) (ContentBlock, error) {
	obj := val.AsObject()
	if obj == nil {
		return nil, fmt.Errorf("block must be an object")
	}
	typeVal, ok := obj["type"]
	if !ok {
		return nil, fmt.Errorf("missing block type")
	}
	typeStr, ok := typeVal.AsString()
	if !ok {
		return nil, fmt.Errorf("type must be a string")
	}

	switch typeStr {
	case "text":
		text, _ := obj["text"].AsString()
		return &TextBlock{Text: text}, nil
	case "tool_use":
		id, _ := obj["id"].AsString()
		name, _ := obj["name"].AsString()
		inputStr, _ := obj["input"].AsString()
		input := unmarshalBlockInputString(inputStr)
		return &ToolUseBlock{ID: id, Name: name, Input: input}, nil
	case "tool_result":
		toolUseID, _ := obj["tool_use_id"].AsString()
		toolName, _ := obj["tool_name"].AsString()
		output, _ := obj["output"].AsString()
		isError := false
		if errVal, ok := obj["is_error"]; ok {
			if b, ok := errVal.AsBool(); ok {
				isError = b
			}
		}
		return &ToolResultBlock{ToolUseID: toolUseID, ToolName: toolName, Content: output, IsError: isError}, nil
	case "system_reminder":
		content, _ := obj["content"].AsString()
		var source string
		if s, ok := obj["source"]; ok {
			source, _ = s.AsString()
		}
		return &SystemReminderBlock{Content: content, Source: source}, nil
	case "image":
		srcVal, ok := obj["source"]
		if !ok {
			return nil, fmt.Errorf("image block missing source")
		}
		srcObj := srcVal.AsObject()
		if srcObj == nil {
			return nil, fmt.Errorf("image source must be an object")
		}
		srcType, _ := srcObj["type"].AsString()
		mediaType, _ := srcObj["media_type"].AsString()
		data, _ := srcObj["data"].AsString()
		return &ImageBlock{
			Type: "image",
			Source: ImageSource{
				Type:      srcType,
				MediaType: mediaType,
				Data:      data,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported block type: %s", typeStr)
	}
}

func usageToJSONValue(u TokenUsage) JsonValue {
	obj := make(map[string]JsonValue)
	obj["input_tokens"] = JsonNumberVal(int64(u.InputTokens))
	obj["output_tokens"] = JsonNumberVal(int64(u.OutputTokens))
	obj["cache_creation_input_tokens"] = JsonNumberVal(int64(u.CacheCreationInputTokens))
	obj["cache_read_input_tokens"] = JsonNumberVal(int64(u.CacheReadInputTokens))
	return JsonObjectVal(obj)
}

func usageFromJSONValue(val JsonValue) (TokenUsage, error) {
	obj := val.AsObject()
	if obj == nil {
		return TokenUsage{}, fmt.Errorf("usage must be an object")
	}
	inputTokens, _ := obj["input_tokens"].AsInt64()
	outputTokens, _ := obj["output_tokens"].AsInt64()
	cacheCreate, _ := obj["cache_creation_input_tokens"].AsInt64()
	cacheRead, _ := obj["cache_read_input_tokens"].AsInt64()
	return TokenUsage{
		InputTokens:              int(inputTokens),
		OutputTokens:             int(outputTokens),
		CacheCreationInputTokens: int(cacheCreate),
		CacheReadInputTokens:     int(cacheRead),
	}, nil
}

// migrate applies version migrations to the session.
func (s *Session) migrate(from, to int) {
	if from < 2 {
		if s.Info == nil {
			now := time.Now()
			s.Info = &SessionInfo{
				Version:   to,
				CreatedAt: &now,
			}
		}
	}
	s.Version = to
	if s.Info != nil {
		s.Info.Version = to
	}
}

// totalTokens sums all usage data across messages.
func (s *Session) totalTokens() int {
	total := 0
	for _, m := range s.Messages {
		if m.Usage != nil {
			total += m.Usage.Total()
		}
	}
	return total
}

// BlockDTO is used for JSON serialization of content blocks.
type BlockDTO struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	RawInput  string                 `json:"input_str,omitempty"` // raw JSON string for tool_use input
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
	Source    string                 `json:"source,omitempty"`
	// Image block fields
	ImageSource *ImageSourceDTO `json:"image_source,omitempty"`
}

// ImageSourceDTO is used for JSON serialization of image source data.
type ImageSourceDTO struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// marshalBlockInput converts a map input to JSON string.
func marshalBlockInput(input map[string]interface{}) string {
	if input == nil {
		return "{}"
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// unmarshalBlockInput parses JSON string to map.
func unmarshalBlockInput(raw string, fallback map[string]interface{}) map[string]interface{} {
	if raw == "" {
		return fallback
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return fallback
	}
	return result
}

// unmarshalBlockInputString parses JSON string to map.
func unmarshalBlockInputString(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(raw), &result)
	return result
}

// Sort helper
var _ = sort.Strings

// --- Managed Session Types (mirrors Rust SessionHandle, ManagedSessionSummary) ---

// SessionHandle is a lightweight reference to a managed session file.
// Mirrors Rust SessionHandle.
type SessionHandle struct {
	ID   string // session ID (derived from filename stem)
	Path string // absolute file path
}

// ManagedSessionSummary holds metadata about a session for listing.
// Mirrors Rust ManagedSessionSummary.
type ManagedSessionSummary struct {
	ID               string
	Path             string
	ModifiedEpochSec int64
	MessageCount     int
	Model            string
}

// SessionsDir returns the sessions directory, creating it if needed.
// Uses <cwd>/.claw/sessions/ matching the Rust convention.
func SessionsDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd + "/.claw/sessions"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// GenerateSessionID creates a unique session ID using millisecond precision.
// Mirrors Rust generate_session_id.
func GenerateSessionID() string {
	return fmt.Sprintf("session-%d", time.Now().UnixMilli())
}

// CreateManagedSessionHandle generates a new session handle.
// Mirrors Rust create_managed_session_handle.
func CreateManagedSessionHandle() (*SessionHandle, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	id := GenerateSessionID()
	return &SessionHandle{
		ID:   id,
		Path: dir + "/" + id + ".json",
	}, nil
}

// ListManagedSessions scans the sessions directory and returns metadata for all sessions.
// Mirrors Rust list_managed_sessions — sorted newest first.
func ListManagedSessions() ([]ManagedSessionSummary, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var summaries []ManagedSessionSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := dir + "/" + entry.Name()
		id := strings.TrimSuffix(entry.Name(), ".json")

		// Try to load lightweight info
		msgCount := 0
		model := ""
		if si, err := LoadSessionInfo(path); err == nil && si != nil {
			msgCount = si.MessageCount
			model = si.Model
		}

		summaries = append(summaries, ManagedSessionSummary{
			ID:               id,
			Path:             path,
			ModifiedEpochSec: info.ModTime().Unix(),
			MessageCount:     msgCount,
			Model:            model,
		})
	}

	// Sort newest first
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ModifiedEpochSec > summaries[j].ModifiedEpochSec
	})

	return summaries, nil
}

// RenderSessionList formats the session list for display, marking the current session.
// Mirrors Rust render_session_list.
func RenderSessionList(sessions []ManagedSessionSummary, currentID string) string {
	if len(sessions) == 0 {
		return "No saved sessions."
	}
	var buf strings.Builder
	for _, s := range sessions {
		marker := "\u25cb saved"
		if s.ID == currentID {
			marker = "\u25cf current"
		}
		t := time.Unix(s.ModifiedEpochSec, 0)
		line := fmt.Sprintf("  %s  %s  (%d msgs)  %s", marker, s.ID, s.MessageCount, t.Format("2006-01-02 15:04"))
		if s.Model != "" {
			line += fmt.Sprintf("  [%s]", s.Model)
		}
		buf.WriteString(line + "\n")
	}
	return buf.String()
}

// ResolveSessionReference resolves a user-provided session reference into a handle.
// Accepts either a direct file path or a session ID.
// Mirrors Rust resolve_session_reference.
func ResolveSessionReference(ref string) (*SessionHandle, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}

	// Try as direct path first
	if _, err := os.Stat(ref); err == nil {
		base := filepath.Base(ref)
		id := strings.TrimSuffix(base, ".json")
		return &SessionHandle{ID: id, Path: ref}, nil
	}

	// Try as session ID in sessions dir
	candidate := dir + "/" + ref
	if !strings.HasSuffix(candidate, ".json") {
		candidate += ".json"
	}
	if _, err := os.Stat(candidate); err == nil {
		id := strings.TrimSuffix(filepath.Base(candidate), ".json")
		return &SessionHandle{ID: id, Path: candidate}, nil
	}

	return nil, fmt.Errorf("session not found: %s", ref)
}
