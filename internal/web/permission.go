package web

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/runtime"
)

// WebPermissionPrompter implements runtime.PermissionPrompter by sending
// a permission_request to the browser over WebSocket and blocking until
// the browser responds or a timeout expires.
type WebPermissionPrompter struct {
	mu         sync.Mutex
	pending    map[string]chan runtime.PermissionPromptDecision
	sendFunc   func([]byte) // push JSON to client's send channel
	promptIDFn func() string
}

// NewWebPermissionPrompter creates a prompter that delegates decisions to the browser.
func NewWebPermissionPrompter(sendFunc func([]byte), idFn func() string) *WebPermissionPrompter {
	return &WebPermissionPrompter{
		pending:    make(map[string]chan runtime.PermissionPromptDecision),
		sendFunc:   sendFunc,
		promptIDFn: idFn,
	}
}

// Decide implements runtime.PermissionPrompter.
// It sends a permission_request envelope to the browser and blocks
// until a permission_response arrives or the 5-minute timeout expires.
func (p *WebPermissionPrompter) Decide(req *runtime.PermissionRequest) runtime.PermissionPromptDecision {
	promptID := p.promptIDFn()
	respCh := make(chan runtime.PermissionPromptDecision, 1)

	p.mu.Lock()
	p.pending[promptID] = respCh
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.pending, promptID)
		p.mu.Unlock()
	}()

	env := Envelope{
		Type:         "permission_request",
		PromptID:     promptID,
		ToolName:     req.ToolName,
		Text:         req.Input,
		CurrentMode:  req.CurrentMode.String(),
		RequiredMode: req.RequiredMode.String(),
	}
	data, err := json.Marshal(env)
	if err == nil {
		p.sendFunc(data)
	}

	select {
	case decision := <-respCh:
		return decision
	case <-time.After(5 * time.Minute):
		return runtime.DecisionDeny
	}
}

// Respond delivers a browser's permission response.
// Called by the WebSocket read pump when a permission_response message arrives.
func (p *WebPermissionPrompter) Respond(promptID string, decision runtime.PermissionPromptDecision) {
	p.mu.Lock()
	ch, ok := p.pending[promptID]
	p.mu.Unlock()
	if ok {
		ch <- decision
	}
}
