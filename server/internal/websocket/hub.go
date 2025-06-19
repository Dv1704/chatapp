package websocket

import (
	"context"
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// DirectMessage represents a message from one client to another
type DirectMessage struct {
	To      string
	From    string
	Content []byte
}

type Hub struct {
	Register      chan *Client
	Unregister    chan *Client
	Clients       map[string]*Client
	Broadcast     chan []byte
	DirectMessage chan DirectMessage
	MaxClients    int

	RedisClient  *redis.Client
	RedisContext context.Context
	Port         string
}

// NewHub initializes a new Hub with required defaults
func NewHub(port string, redisClient *redis.Client) *Hub {
	return &Hub{
		Register:      make(chan *Client),
		Unregister:    make(chan *Client),
		Clients:       make(map[string]*Client),
		Broadcast:     make(chan []byte),
		DirectMessage: make(chan DirectMessage),
		MaxClients:    100,
		Port:          port,
		RedisClient:   redisClient,
		RedisContext:  context.Background(),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if len(h.Clients) >= h.MaxClients {
				client.Conn.WriteMessage(websocket.TextMessage, []byte("❌ Server is full. Try again later."))
				client.Conn.Close()
				continue
			}
			h.Clients[client.Username] = client
			log.Printf("✅ Registered: %s | Clients: %d", client.Username, len(h.Clients))
			h.broadcastUserList()

		case client := <-h.Unregister:
			if _, ok := h.Clients[client.Username]; ok {
				delete(h.Clients, client.Username)
				close(client.Send)
				log.Printf("❌ Unregistered: %s | Clients: %d", client.Username, len(h.Clients))
				h.broadcastUserList()
			}

		case message := <-h.Broadcast:
			for _, client := range h.Clients {
				select {
				case client.Send <- message:
				default:
					log.Printf("⚠️ Broadcast failed, closing: %s", client.Username)
					close(client.Send)
					delete(h.Clients, client.Username)
				}
			}

		case dm := <-h.DirectMessage:
			if client, ok := h.Clients[dm.To]; ok {
				msg := Message{
					Type:    "direct",
					From:    dm.From,
					To:      dm.To,
					Content: string(dm.Content),
				}
				msgBytes, err := json.Marshal(msg)
				if err != nil {
					log.Printf("❌ Marshal error for direct message to %s: %v", dm.To, err)
					continue
				}
				select {
				case client.Send <- msgBytes:
					log.Printf("📤 Direct message: %s ➝ %s", dm.From, dm.To)
				default:
					log.Printf("⚠️ Direct message send failed, closing: %s", client.Username)
					close(client.Send)
					delete(h.Clients, client.Username)
				}
			} else {
				log.Printf("🚫 Direct message target not found: %s (from %s)", dm.To, dm.From)
			}
		}
	}
}

func (h *Hub) broadcastUserList() {
	usernames := make([]string, 0, len(h.Clients))
	for username := range h.Clients {
		usernames = append(usernames, username)
	}

	msg := Message{
		Type:    "user_list",
		Content: usernames,
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("❌ Error marshalling user list: %v", err)
		return
	}

	for _, client := range h.Clients {
		select {
		case client.Send <- msgBytes:
		default:
			log.Printf("⚠️ Failed to send user list to %s, closing...", client.Username)
			close(client.Send)
			delete(h.Clients, client.Username)
		}
	}
}
