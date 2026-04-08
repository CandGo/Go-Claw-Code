package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CandGo/Go-Claw-Code/internal/api"
)

// MakeLLMCompactionFunc returns an LLM-based summarization function suitable
// for use with SetLLMSummarizeFunc. It sends the messages to the model with a
// summarization prompt and returns the result.
func MakeLLMCompactionFunc(provider api.Provider, model string) func(ctx context.Context, messages []ConversationMessage, existingSummary string) (string, error) {
	return func(ctx context.Context, messages []ConversationMessage, existingSummary string) (string, error) {
		var parts []string
		parts = append(parts, "Summarize the following conversation messages. Focus on: what was requested, what tools were used, what files were touched, what the current state of work is, and any pending tasks.")
		if existingSummary != "" {
			parts = append(parts, fmt.Sprintf("\nExisting summary from prior compaction:\n%s", existingSummary))
		}
		parts = append(parts, "\nMessages to summarize:")

		for _, m := range messages {
			role := string(m.Role)
			for _, b := range m.Content {
				switch v := b.(type) {
				case *TextBlock:
					parts = append(parts, fmt.Sprintf("[%s]: %s", role, truncateUnicode(v.Text, 500)))
				case *ToolUseBlock:
					inputJSON, _ := json.Marshal(v.Input)
					parts = append(parts, fmt.Sprintf("[%s]: tool_use %s(%s)", role, v.Name, truncateUnicode(string(inputJSON), 300)))
				case *ToolResultBlock:
					parts = append(parts, fmt.Sprintf("[%s]: tool_result %s: %s", role, v.ToolName, truncateUnicode(v.Content, 300)))
				}
			}
		}

		parts = append(parts, "\nProvide a concise summary in <summary> tags.")

		prompt := ""
		for i, p := range parts {
			if i > 0 {
				prompt += "\n"
			}
			prompt += p
		}

		sysPrompt, _ := json.Marshal("You are a conversation summarizer. Produce concise, factual summaries that capture key context for continuing the conversation.")
		req := &api.MessageRequest{
			Model:     model,
			MaxTokens: 1024,
			Messages: []api.InputMessage{
				{Role: api.RoleUser, Content: []api.InputContentBlock{
					{Type: "text", Text: prompt},
				}},
			},
			System: sysPrompt,
		}

		resp, err := provider.SendMessage(ctx, req)
		if err != nil {
			return "", fmt.Errorf("LLM compaction failed: %w", err)
		}

		if len(resp.Content) > 0 {
			return resp.Content[0].Text, nil
		}
		return "", fmt.Errorf("LLM compaction: empty response")
	}
}
