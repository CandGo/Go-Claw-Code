package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var clientCounter atomic.Int64

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Hub manages all active WebSocket clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

// Register adds a client to the hub.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c.id] = c
	h.mu.Unlock()
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c.id)
	h.mu.Unlock()
}

// Client represents a single WebSocket connection.
type Client struct {
	id      string
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	session *ClientSession
}

// newClient creates a Client for a WebSocket connection.
func newClient(conn *websocket.Conn, hub *Hub, sessionCfg *SessionConfig) *Client {
	id := fmt.Sprintf("client-%d", clientCounter.Add(1))
	send := make(chan []byte, 4096)

	client := &Client{
		id:   id,
		hub:  hub,
		conn: conn,
		send: send,
	}

	client.session = NewClientSession(sessionCfg, func(data []byte) {
		send <- data
	})

	return client
}

// readPump reads messages from the WebSocket connection and dispatches them.
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.session.Close()
		c.conn.Close()
	}()

	c.conn.SetReadLimit(1 << 20) // 1MB max message
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("web: read error: %v", err)
			}
			break
		}

		var env Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("web: unmarshal error: %v", err)
			continue
		}

		c.session.HandleMessage(env)
	}
}

// writePump sends queued messages to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Drain queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
