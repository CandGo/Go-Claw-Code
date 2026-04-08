package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed assets/*
var assetsFS embed.FS

// WebServer serves the web UI and WebSocket endpoint.
type WebServer struct {
	hub       *Hub
	sessionCfg *SessionConfig
	addr      string
	version   string
	server    *http.Server
}

// NewWebServer creates a new WebServer.
func NewWebServer(cfg *SessionConfig, addr, version string) *WebServer {
	return &WebServer{
		hub:        NewHub(),
		sessionCfg: cfg,
		addr:       addr,
		version:    version,
	}
}

// Start launches the HTTP server (blocking).
func (s *WebServer) Start() error {
	mux := http.NewServeMux()

	// Serve embedded frontend assets
	assetsSub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(assetsSub))
	mux.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	mux.HandleFunc("/", s.handleIndex)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("web: server starting on http://%s", s.addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *WebServer) Shutdown() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("web: upgrade error: %v", err)
		return
	}

	client := newClient(conn, s.hub, s.sessionCfg)
	s.hub.Register(client)

	// Send connected message
	connected := Envelope{
		Type:           "connected",
		Model:          s.sessionCfg.Model,
		Version:        s.version,
		PermissionMode: s.sessionCfg.PermMode,
	}
	if data, err := json.Marshal(connected); err == nil {
		client.send <- data
	}

	// Start pumps
	go client.writePump()
	go client.readPump()
}

func (s *WebServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"model":   s.sessionCfg.Model,
		"clients": s.hub.ClientCount(),
	})
}

// ClientCount returns the number of connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
