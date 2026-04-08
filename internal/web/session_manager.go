package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/api"
	"github.com/CandGo/Go-Claw-Code/internal/runtime"
	"github.com/CandGo/Go-Claw-Code/internal/tools"
)

var sessionCounter atomic.Int64

// SessionConfig holds the shared configuration for creating per-client runtimes.
type SessionConfig struct {
	Provider     api.Provider
	ToolRegistry *tools.ToolRegistry
	Model        string
	PermMode     string
	Hooks        *runtime.HookRunner
}

// ClientSession wraps a ConversationRuntime for a single WebSocket client.
type ClientSession struct {
	id       string
	rt       *runtime.ConversationRuntime
	cancelFn context.CancelFunc
	mu       sync.Mutex
	prompter *WebPermissionPrompter
	send     func([]byte)
	cfg      *SessionConfig
}

// NewClientSession creates an independent runtime for a WebSocket client.
func NewClientSession(cfg *SessionConfig, send func([]byte)) *ClientSession {
	id := fmt.Sprintf("session-%d", sessionCounter.Add(1))
	rt := buildRuntime(cfg, send)

	prompter := NewWebPermissionPrompter(send, func() string {
		return fmt.Sprintf("perm-%d", time.Now().UnixNano())
	})
	rt.SetPermissionPrompter(prompter)

	return &ClientSession{
		id:       id,
		rt:       rt,
		prompter: prompter,
		send:     send,
		cfg:      cfg,
	}
}

func buildRuntime(cfg *SessionConfig, send func([]byte)) *runtime.ConversationRuntime {
	rt := runtime.NewConversationRuntime(cfg.Provider, cfg.ToolRegistry, cfg.Model)
	if cfg.Hooks != nil {
		rt.SetHooks(cfg.Hooks)
	}
	if cfg.PermMode != "" {
		rt.SetPermissionMode(cfg.PermMode)
	}
	return rt
}

// HandleMessage processes an incoming WebSocket envelope.
func (cs *ClientSession) HandleMessage(env Envelope) {
	switch env.Type {
	case "message":
		cs.handleUserMessage(env)
	case "permission_response":
		cs.handlePermissionResponse(env)
	case "cancel":
		cs.handleCancel()
	case "session_new":
		cs.handleNewSession()
	case "session_list":
		cs.handleSessionList()
	case "session_clear":
		cs.handleClear()
	case "command":
		cs.handleCommand(env)
	default:
		cs.sendError(env.ID, fmt.Sprintf("unknown message type: %s", env.Type))
	}
}

func (cs *ClientSession) handleUserMessage(env Envelope) {
	if !cs.mu.TryLock() {
		cs.sendError(env.ID, "a turn is already in progress")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cs.cancelFn = cancel

	outCh := make(chan runtime.TurnOutput, 256)
	usageCh := make(chan runtime.TokenUsage, 1)
	errCh := make(chan error, 1)

	go cs.rt.RunTurnStreaming(ctx, env.Content, outCh, usageCh, errCh)

	go func() {
		defer cs.mu.Unlock()
		defer cancel()

		// Buffer text deltas and flush every 60ms to reduce WS message count
		var textBuf string
		flushText := func() {
			if textBuf == "" {
				return
			}
			cs.sendEnvelope(Envelope{ID: env.ID, Type: "text_delta", Text: textBuf})
			textBuf = ""
		}

		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case out, ok := <-outCh:
				if !ok {
					// Channel closed — turn finished
					flushText()

					select {
					case usage := <-usageCh:
						cs.sendEnvelope(Envelope{
							Type: "turn_done",
							ID:   env.ID,
							Usage: &UsageInfo{
								InputTokens:  usage.InputTokens,
								OutputTokens: usage.OutputTokens,
							},
						})
					default:
						cs.sendEnvelope(Envelope{Type: "turn_done", ID: env.ID})
					}

					select {
					case err := <-errCh:
						if err != nil {
							cs.sendError(env.ID, err.Error())
						}
					default:
					}
					return
				}

				// Buffer text/thinking deltas; send others immediately
				switch out.Type {
				case "text_delta", "thinking_delta":
					textBuf += out.Text
				default:
					flushText()
					cs.sendTurnOutput(env.ID, out)
				}

			case <-ticker.C:
				flushText()
			}
		}
	}()
}

func (cs *ClientSession) handlePermissionResponse(env Envelope) {
	var decision runtime.PermissionPromptDecision
	switch env.Decision {
	case "allow":
		decision = runtime.DecisionAllow
	case "allow_always":
		decision = runtime.DecisionAllowAlways
	default:
		decision = runtime.DecisionDeny
	}
	cs.prompter.Respond(env.PromptID, decision)
}

func (cs *ClientSession) handleCancel() {
	if cs.cancelFn != nil {
		cs.cancelFn()
	}
}

func (cs *ClientSession) handleNewSession() {
	cs.rt = buildRuntime(cs.cfg, cs.send)
	newID := fmt.Sprintf("session-%d", sessionCounter.Add(1))
	cs.id = newID

	prompter := NewWebPermissionPrompter(cs.send, func() string {
		return fmt.Sprintf("perm-%d", time.Now().UnixNano())
	})
	cs.prompter = prompter
	cs.rt.SetPermissionPrompter(prompter)

	cs.sendEnvelope(Envelope{
		Type:         "session_info",
		SessionID:    newID,
		Model:        cs.rt.Model(),
		MessageCount: 0,
	})
}

func (cs *ClientSession) handleSessionList() {
	cs.sendEnvelope(Envelope{Type: "session_list", Sessions: []SessionEntry{}})
}

func (cs *ClientSession) handleClear() {
	cs.rt.Clear()
	cs.sendEnvelope(Envelope{
		Type:         "session_info",
		SessionID:    cs.id,
		Model:        cs.rt.Model(),
		MessageCount: 0,
	})
}

func (cs *ClientSession) handleCommand(env Envelope) {
	cmd := env.Command
	switch {
	case cmd == "/compact":
		result := cs.rt.Compact(runtime.DefaultCompactionConfig())
		cs.sendEnvelope(Envelope{
			Type:    "command_output",
			Command: cmd,
			Output:  fmt.Sprintf("Compacted: %d -> %d messages", result.MessagesBefore, result.MessagesAfter),
		})
	case cmd == "/clear":
		cs.rt.Clear()
		cs.sendEnvelope(Envelope{
			Type:    "command_output",
			Command: cmd,
			Output:  "Session cleared",
		})
	case cmd == "/model":
		if env.Args != "" {
			cs.rt.SetModel(env.Args)
			cs.sendEnvelope(Envelope{Type: "command_output", Command: cmd, Output: "Model set to " + env.Args})
		} else {
			cs.sendEnvelope(Envelope{Type: "command_output", Command: cmd, Output: cs.rt.Model()})
		}
	default:
		cs.sendEnvelope(Envelope{
			Type:    "command_output",
			Command: cmd,
			Output:  fmt.Sprintf("Command %s executed", cmd),
		})
	}
}

func (cs *ClientSession) sendTurnOutput(msgID string, out runtime.TurnOutput) {
	env := Envelope{ID: msgID}
	switch out.Type {
	case "tool_use":
		env.Type = "tool_use"
		env.ToolID = out.ToolID
		env.ToolName = out.ToolName
		env.ToolInput = out.ToolInput
	case "tool_result":
		env.Type = "tool_result"
		env.ToolID = out.ToolID
		env.ToolName = out.ToolName
		env.Text = out.Text
		env.IsError = out.IsError
	case "done":
		return
	default:
		return
	}
	cs.sendEnvelope(env)
}

func (cs *ClientSession) sendEnvelope(env Envelope) {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("web: marshal error: %v", err)
		return
	}
	cs.send(data)
}

func (cs *ClientSession) sendError(id, msg string) {
	cs.sendEnvelope(Envelope{Type: "error", ID: id, Message: msg})
}

// Close cleans up the session.
func (cs *ClientSession) Close() {
	if cs.cancelFn != nil {
		cs.cancelFn()
	}
}
