package tui

import (
	"fmt"
	"strings"
	"sync"
)

// StreamingState tracks the state of an incremental streaming response.
type StreamingState struct {
	mu       sync.Mutex
	text     strings.Builder
	tools    []streamingTool
	thinking strings.Builder
	done     bool
	err      error
}

type streamingTool struct {
	Name   string
	Input  string
	Result string
	Err    error
	Done   bool
}

// NewStreamingState creates a new streaming state.
func NewStreamingState() *StreamingState {
	return &StreamingState{}
}

// AppendText adds streaming text.
func (s *StreamingState) AppendText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text.WriteString(text)
}

// AppendThinking adds thinking block text.
func (s *StreamingState) AppendThinking(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thinking.WriteString(text)
}

// AddTool adds a tool call to the streaming state.
func (s *StreamingState) AddTool(name, input string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, streamingTool{
		Name:  name,
		Input: input,
	})
}

// CompleteTool marks a tool as complete with its result.
func (s *StreamingState) CompleteTool(name, result string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tools {
		if s.tools[i].Name == name && !s.tools[i].Done {
			s.tools[i].Result = result
			s.tools[i].Err = err
			s.tools[i].Done = true
			return
		}
	}
}

// Finish marks the streaming as complete.
func (s *StreamingState) Finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	s.err = err
}

// GetText returns the accumulated streaming text.
func (s *StreamingState) GetText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.String()
}

// GetThinking returns the accumulated thinking text.
func (s *StreamingState) GetThinking() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thinking.String()
}

// IsDone returns whether streaming is complete.
func (s *StreamingState) IsDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// GetPendingTools returns tools that are still executing.
func (s *StreamingState) GetPendingTools() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []string
	for _, t := range s.tools {
		if !t.Done {
			result = append(result, t.Name)
		}
	}
	return result
}

// ToolStatusLine generates a status line for the current streaming state.
func (s *StreamingState) ToolStatusLine() string {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return ""
	}
	// Collect pending tools while holding the lock
	var pending []string
	for _, t := range s.tools {
		if !t.Done {
			pending = append(pending, t.Name)
		}
	}
	s.mu.Unlock()

	if len(pending) > 0 {
		return fmt.Sprintf("  Executing: %s", strings.Join(pending, ", "))
	}
	return ""
}
