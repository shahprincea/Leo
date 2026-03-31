package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// Broadcaster is the interface WatchHandler uses to emit real-time events.
// The in-memory Hub implements this; tests can substitute a fake.
type Broadcaster interface {
	Broadcast(wearerID string, event any)
}

// Hub manages active WebSocket connections keyed by wearer subscription.
// One client (companion app user) can subscribe to multiple wearers.
type Hub struct {
	mu      sync.RWMutex
	clients map[string][]*wsClient // wearerID → clients subscribed to it
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string][]*wsClient)}
}

// Broadcast sends event to every companion-app client subscribed to wearerID.
func (h *Hub) Broadcast(wearerID string, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*wsClient, len(h.clients[wearerID]))
	copy(clients, h.clients[wearerID])
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Drop the event if the client's buffer is full (slow consumer).
		}
	}
}

func (h *Hub) subscribe(wearerID string, c *wsClient) {
	h.mu.Lock()
	h.clients[wearerID] = append(h.clients[wearerID], c)
	h.mu.Unlock()
}

func (h *Hub) unsubscribeAll(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for wearerID, list := range h.clients {
		filtered := list[:0]
		for _, existing := range list {
			if existing != c {
				filtered = append(filtered, existing)
			}
		}
		if len(filtered) == 0 {
			delete(h.clients, wearerID)
		} else {
			h.clients[wearerID] = filtered
		}
	}
}

// wsClient represents one connected companion app session.
type wsClient struct {
	conn   *websocket.Conn
	userID string
	send   chan []byte
}

// WSHandler upgrades HTTP connections to WebSocket and manages the lifecycle.
type WSHandler struct {
	hub    *Hub
	cfg    *config.Config
	wearDB WearerRepository
}

// NewWSHandler creates a WSHandler.
func NewWSHandler(hub *Hub, cfg *config.Config, wearDB WearerRepository) *WSHandler {
	return &WSHandler{hub: hub, cfg: cfg, wearDB: wearDB}
}

var upgrader = websocket.Upgrader{
	// Allow all origins for now; tighten in production via allowed-origins config.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS handles GET /ws?token=<access_token>.
func (h *WSHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	claims, err := auth.VerifyAccessToken(token, h.cfg.JWTSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade writes its own error response
	}

	c := &wsClient{
		conn:   conn,
		userID: claims.UserID,
		send:   make(chan []byte, 64),
	}

	go h.writePump(c)
	h.readPump(c) // blocks until client disconnects
}

// readPump processes inbound messages from a single WebSocket client.
// Expected inbound message: { "type": "subscribe", "wearer_id": "<uuid>" }
func (h *WSHandler) readPump(c *wsClient) {
	defer func() {
		h.hub.unsubscribeAll(c)
		c.conn.Close()
		close(c.send)
	}()

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg struct {
			Type     string `json:"type"`
			WearerID string `json:"wearer_id"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		if msg.Type != "subscribe" || msg.WearerID == "" {
			continue
		}

		// Verify the user is a member of this wearer before subscribing.
		membership, err := h.wearDB.GetWearerMembership(context.Background(), msg.WearerID, c.userID)
		if err != nil || membership == nil {
			_ = c.conn.WriteJSON(map[string]string{"type": "error", "error": "forbidden"})
			continue
		}
		h.hub.subscribe(msg.WearerID, c)
		_ = c.conn.WriteJSON(map[string]string{"type": "subscribed", "wearer_id": msg.WearerID})
	}
}

// writePump drains the client's send channel and writes to the WebSocket connection.
func (h *WSHandler) writePump(c *wsClient) {
	for data := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}
