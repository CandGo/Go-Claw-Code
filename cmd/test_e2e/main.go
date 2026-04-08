package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/api"
	clawauth "github.com/CandGo/Go-Claw-Code/internal/auth"
)

func main() {
	apiKey, _, _ := clawauth.ResolveAuthWithOAuth()
	authSrc := &api.AuthSource{APIKey: apiKey}
	provider := api.NewProvider("claude-sonnet-4-20250514", authSrc, api.BaseURL())

	req := &api.MessageRequest{
		Model:  "claude-sonnet-4-20250514",
		MaxTokens: 64,
		Messages: []api.InputMessage{
			{Role: api.RoleUser, Content: []api.InputContentBlock{
				{Type: "text", Text: "Say: hi"},
			}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	framesCh, err := provider.StreamMessage(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stream error: %v\n", err)
		os.Exit(1)
	}

	for frame := range framesCh {
		var raw map[string]interface{}
		json.Unmarshal([]byte(frame.Data), &raw)
		eventType, _ := raw["type"].(string)
		if eventType == "message_start" {
			if msg, ok := raw["message"].(map[string]interface{}); ok {
				if u, ok := msg["usage"].(map[string]interface{}); ok {
					pretty, _ := json.MarshalIndent(u, "", "  ")
					fmt.Printf("usage from message_start:\n%s\n", string(pretty))
				} else {
					fmt.Printf("message_start (no usage): %v\n", msg)
				}
			}
		}
		if eventType == "message_delta" {
			pretty, _ := json.MarshalIndent(raw, "", "  ")
			fmt.Printf("message_delta:\n%s\n", string(pretty))
		}
	}
}
