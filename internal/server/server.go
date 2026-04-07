package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-claw/claw/internal/api"
	"github.com/go-claw/claw/internal/runtime"
)

// Server provides an HTTP/SSE API for Go-Claw-Code.
type Server struct {
	rt       *runtime.ConversationRuntime
	port     int
	server   *http.Server
}

// NewServer creates a new HTTP server.
func NewServer(rt *runtime.ConversationRuntime, port int) *Server {
	return &Server{
		rt:   rt,
		port: port,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/v1/messages/stream", s.handleMessagesStream)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/session", s.handleSession)
	mux.HandleFunc("/session/clear", s.handleSessionClear)
	mux.HandleFunc("/tools", s.handleTools)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: corsMiddleware(mux),
	}

	log.Printf("Claw server starting on port %d", s.port)
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleMessages handles POST /v1/messages (non-streaming).
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	outputs, usage, err := s.rt.RunTurn(ctx, req.Prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "runtime_error", err.Error())
		return
	}

	resp := map[string]interface{}{
		"outputs": outputs,
		"usage":   usage,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleMessagesStream handles POST /v1/messages/stream (SSE streaming).
// Uses a channel-based approach so that each TurnOutput is sent to the
// client as soon as it is produced by the runtime, instead of waiting for
// the entire turn to complete.
func (s *Server) handleMessagesStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Buffered channels so the runtime goroutine is not blocked by slow writes.
	outCh := make(chan runtime.TurnOutput, 16)
	usageCh := make(chan runtime.TokenUsage, 1)
	errCh := make(chan error, 1)

	// Launch the turn in a goroutine.  Each TurnOutput is sent to outCh as
	// soon as it is produced, giving us true streaming behaviour.
	go s.rt.RunTurnStreaming(ctx, req.Prompt, outCh, usageCh, errCh)

	// Read outputs as they arrive and write SSE events immediately.
	for out := range outCh {
		data, _ := json.Marshal(out)
		fmt.Fprintf(w, "event: output\ndata: %s\n\n", data)
		flusher.Flush()
	}

	// Send usage if available.
	select {
	case usage := <-usageCh:
		data, _ := json.Marshal(usage)
		fmt.Fprintf(w, "event: usage\ndata: %s\n\n", data)
		flusher.Flush()
	default:
	}

	// Send error if the turn failed.
	select {
	case err := <-errCh:
		if err != nil {
			data, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
			flusher.Flush()
		}
	default:
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// handleHealth returns server health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"model":        s.rt.Model(),
		"messages":     s.rt.MessageCount(),
	})
}

// handleSession returns session info.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model":    s.rt.Model(),
		"messages": s.rt.MessageCount(),
	})
}

// handleSessionClear clears the session.
func (s *Server) handleSessionClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.rt.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleTools returns available tools.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	// This would need the tool registry accessible
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": []string{},
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, anthropic-version")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// ProxyHandler proxies requests to the Anthropic API, adding auth.
func ProxyHandler(auth *api.AuthSource, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetURL := baseURL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "proxy_error", err.Error())
			return
		}

		// Copy headers
		for k, vs := range r.Header {
			for _, v := range vs {
				proxyReq.Header.Add(k, v)
			}
		}

		// Add auth
		if auth.APIKey != "" {
			proxyReq.Header.Set("x-api-key", auth.APIKey)
		}
		if auth.BearerToken != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+auth.BearerToken)
		}

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(proxyReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}

		w.WriteHeader(resp.StatusCode)

		// Stream response body
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			flusher, ok := w.(http.Flusher)
			if ok {
				buf := make([]byte, 4096)
				for {
					n, err := resp.Body.Read(buf)
					if n > 0 {
						w.Write(buf[:n])
						flusher.Flush()
					}
					if err == io.EOF || err != nil {
						break
					}
				}
				return
			}
		}

		io.Copy(w, resp.Body)
	}
}
