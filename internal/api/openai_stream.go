package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// oaiStreamChunk represents an OpenAI streaming chunk.
type oaiStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index        int            `json:"index"`
		Delta        oaiStreamDelta `json:"delta"`
		FinishReason *string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaiStreamUsage `json:"usage,omitempty"`
}

type oaiStreamDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   *string          `json:"content,omitempty"`
	ToolCalls []oaiStreamTool  `json:"tool_calls,omitempty"`
}

type oaiStreamTool struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function oaiStreamFuncCall `json:"function"`
}

type oaiStreamFuncCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type oaiStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// translateOpenAIStream reads OpenAI SSE chunks and emits Anthropic-format SSEFrames.
// This is the key bridge: OpenAI uses choices[0].delta.content and choices[0].delta.tool_calls,
// while Anthropic uses content_block_start/delta/stop events.
func translateOpenAIStream(r io.Reader) <-chan SSEFrame {
	ch := make(chan SSEFrame, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// State for accumulating tool calls across chunks
		type toolAccum struct {
			ID        strings.Builder
			Name      strings.Builder
			Arguments strings.Builder
			Started   bool
		}
		toolAccums := make(map[int]*toolAccum)
		contentStarted := false
		msgID := "msg_openai"
		inputTokens := 0

		for scanner.Scan() {
			line := scanner.Text()

			// SSE lines start with "data: "
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			// Stream end
			if data == "[DONE]" {
				// Flush any pending content
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
				}
				// Flush any pending tool calls
				for idx, ta := range toolAccums {
					if ta.Started {
						ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
							`{"type":"content_block_stop","index":%d}`, idx+1)}
					}
				}
				ch <- SSEFrame{Event: "event", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`}
				ch <- SSEFrame{Event: "event", Data: `{"type":"message_stop"}`}
				return
			}

			var chunk oaiStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.ID != "" {
				msgID = chunk.ID
			}

			// Usage from final chunk
			if chunk.Usage != nil {
				inputTokens = chunk.Usage.PromptTokens
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]

			// Emit message_start on first chunk with role
			if choice.Delta.Role == "assistant" && !contentStarted && len(toolAccums) == 0 {
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","usage":{"input_tokens":%d,"output_tokens":0}}}`,
					msgID, inputTokens)}
			}

			// Handle text content
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				text := *choice.Delta.Content
				if !contentStarted {
					contentStarted = true
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`}
				}
				escaped, _ := json.Marshal(text)
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`,
					string(escaped))}
			}

			// Handle tool calls
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				if toolAccums[idx] == nil {
					toolAccums[idx] = &toolAccum{}
				}
				ta := toolAccums[idx]

				// Close text block if still open
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
					contentStarted = false
				}

				// New tool call: emit content_block_start
				if tc.ID != "" && !ta.Started {
					ta.Started = true
					ta.ID.WriteString(tc.ID)
					if tc.Function.Name != "" {
						ta.Name.WriteString(tc.Function.Name)
					}
					blockIdx := idx + 1 // text is index 0
					name, _ := json.Marshal(ta.Name.String())
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"%s","name":%s,"input":{}}}`,
						blockIdx, tc.ID, string(name))}
				}

				// Accumulate arguments
				if tc.Function.Arguments != "" {
					ta.Arguments.WriteString(tc.Function.Arguments)
					blockIdx := idx + 1
					escaped, _ := json.Marshal(tc.Function.Arguments)
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
						blockIdx, string(escaped))}
				}
			}

			// Handle finish
			if choice.FinishReason != nil {
				reason := *choice.FinishReason
				// Map OpenAI finish reasons to Anthropic stop reasons
				stopReason := "end_turn"
				if reason == "tool_calls" {
					stopReason = "tool_use"
				}

				// Close content block if open
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
					contentStarted = false
				}

				// Close any open tool call blocks
				for idx, ta := range toolAccums {
					if ta.Started {
						ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
							`{"type":"content_block_stop","index":%d}`, idx+1)}
						ta.Started = false
					}
				}

				escaped, _ := json.Marshal(stopReason)
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"message_delta","delta":{"stop_reason":%s},"usage":{"output_tokens":0}}`,
					string(escaped))}
				ch <- SSEFrame{Event: "event", Data: `{"type":"message_stop"}`}
				return
			}
		}
	}()
	return ch
}
