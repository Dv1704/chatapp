package server

import (
	"net/http"
	"time"

	ws "github.com/dv1704/chatapp/internal/websocket"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Correct ServeWS function signature
func ServeWS(hub *ws.Hub, pubsub *ws.RedisPubSub, w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "WebSocket upgrade failed", http.StatusInternalServerError)
		return
	}

	// Extract the username from the query string
	username := r.URL.Query().Get("username")
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		conn.Close()
		return
	}

	// Create new client with username and inject RedisPubSub
	client := &ws.Client{
		Conn:         conn,
		Send:         make(chan []byte),
		Hub:          hub,
		Username:     username,
		MessageCount: 0,
		LimitReset:   time.NewTicker(1 * time.Second),
		RedisPubSub:  pubsub, // Inject Redis pub/sub for broadcasting
	}

	// Register the client in the Hub
	hub.Register <- client

	// Start listening for incoming and outgoing messages
	go client.ReadPump(hub)
	go client.WritePump()
}