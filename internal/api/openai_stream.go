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
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	ToolCalls []oaiStreamTool `json:"tool_calls,omitempty"`
	// Reasoning content for o1/o3/DeepSeek-R1 models
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	Reasoning        *string `json:"reasoning,omitempty"`
}

type oaiStreamTool struct {
	Index   int    `json:"index"`
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type oaiStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// toolCallAccum accumulates incremental tool call deltas across chunks.
type toolCallAccum struct {
	id        strings.Builder
	name      strings.Builder
	arguments strings.Builder
	started   bool
}

// translateOpenAIStream reads OpenAI SSE chunks and emits Anthropic-format SSEFrames.
// Supports reasoning_content/reasoning fields from o1, o3, DeepSeek-R1, etc.
func translateOpenAIStream(r io.Reader) <-chan SSEFrame {
	ch := make(chan SSEFrame, 64)
	go func() {
		defer close(ch)
		if closer, ok := r.(io.Closer); ok {
			defer closer.Close()
		}
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		_ = 0 // frameCount unused but kept for compatibility

		toolAccums := make(map[int]*toolCallAccum)
		contentStarted := false
		reasoningStarted := false
		messageStarted := false
		textBlockIndex := 0 // 0 if no reasoning, 1 if reasoning came first
		msgID := "msg_openai"
		inputTokens := 0
		outputTokens := 0

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			// Stream end
			if data == "[DONE]" {
				if reasoningStarted {
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_stop","index":0}`)}
				}
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_stop","index":%d}`, textBlockIndex)}
				}
				for idx, ta := range toolAccums {
					if ta.started {
						ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
							`{"type":"content_block_stop","index":%d}`, idx+1)}
					}
				}
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":%d}}`, outputTokens)}
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
			if chunk.Usage != nil {
				inputTokens = chunk.Usage.PromptTokens
				outputTokens = chunk.Usage.CompletionTokens
			}

			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]

			// Emit message_start on first chunk with role
			if choice.Delta.Role == "assistant" && !messageStarted {
				messageStarted = true
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","usage":{"input_tokens":%d,"output_tokens":0}}}`,
					msgID, inputTokens)}
			}

			// Reasoning/thinking content (o1, o3, DeepSeek-R1, etc.)
			var reasoningText string
			if choice.Delta.ReasoningContent != nil {
				reasoningText = *choice.Delta.ReasoningContent
			} else if choice.Delta.Reasoning != nil {
				reasoningText = *choice.Delta.Reasoning
			}
			if reasoningText != "" {
				if !reasoningStarted {
					reasoningStarted = true
					textBlockIndex = 1 // text will be at index 1 after reasoning
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`}
				}
				escaped, _ := json.Marshal(reasoningText)
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":%s}}`,
					string(escaped))}
			}

			// Text content
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				text := *choice.Delta.Content
				// Close reasoning block if transitioning to text
				if reasoningStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
					reasoningStarted = false
					textBlockIndex = 1
				}
				if !contentStarted {
					contentStarted = true
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, textBlockIndex)}
				}
				escaped, _ := json.Marshal(text)
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`,
					textBlockIndex, string(escaped))}
			}

			// Tool calls
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				if toolAccums[idx] == nil {
					toolAccums[idx] = &toolCallAccum{}
				}
				ta := toolAccums[idx]

				// Close reasoning block if open
				if reasoningStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
					reasoningStarted = false
				}
				// Close text block if still open
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_stop","index":%d}`, textBlockIndex)}
					contentStarted = false
				}

				// New tool call: emit content_block_start
				if tc.ID != "" && !ta.started {
					ta.started = true
					ta.id.WriteString(tc.ID)
					if tc.Function.Name != "" {
						ta.name.WriteString(tc.Function.Name)
					}
					blockIdx := idx + 1
					escapedName, _ := json.Marshal(ta.name.String())
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"%s","name":%s,"input":{}}}`,
						blockIdx, tc.ID, string(escapedName))}
				}

				// Accumulate arguments
				if tc.Function.Arguments != "" {
					ta.arguments.WriteString(tc.Function.Arguments)
					blockIdx := idx + 1
					escaped, _ := json.Marshal(tc.Function.Arguments)
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
						blockIdx, string(escaped))}
				}
			}

			// Handle finish reason
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				reason := *choice.FinishReason
				stopReason := "end_turn"
				if reason == "tool_calls" {
					stopReason = "tool_use"
				}

				// Close reasoning block if open
				if reasoningStarted {
					ch <- SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
					reasoningStarted = false
				}
				// Close content block if open
				if contentStarted {
					ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
						`{"type":"content_block_stop","index":%d}`, textBlockIndex)}
					contentStarted = false
				}

				// Close any open tool call blocks
				for idx, ta := range toolAccums {
					if ta.started {
						ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
							`{"type":"content_block_stop","index":%d}`, idx+1)}
						ta.started = false
					}
				}

				escaped, _ := json.Marshal(stopReason)
				ch <- SSEFrame{Event: "event", Data: fmt.Sprintf(
					`{"type":"message_delta","delta":{"stop_reason":%s},"usage":{"output_tokens":%d}}`,
					string(escaped), outputTokens)}
				ch <- SSEFrame{Event: "event", Data: `{"type":"message_stop"}`}
				return
			}
		}
	}()
	return ch
}
