package websocket

import (
	"encoding/json"
	"log"

	"context"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// DirectMessage struct to encapsulate private message details
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
	RedisClient *redis.Client
	RedisContext context.Context
}

// NewHub initializes and returns a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		Register:      make(chan *Client),
		Unregister:    make(chan *Client),
		Clients:       make(map[string]*Client),
		Broadcast:     make(chan []byte),
		DirectMessage: make(chan DirectMessage),
		MaxClients:    100,
		RedisClient:  nil,
		RedisContext: nil, // Initialize context to nil as well
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			if len(h.Clients) >= h.MaxClients {
				client.Conn.WriteMessage(websocket.TextMessage, []byte("Server is full. Try again later."))
				client.Conn.Close()
				continue
			}
			h.Clients[client.Username] = client
			log.Printf("Client %s registered. Total clients: %d", client.Username, len(h.Clients))
			h.broadcastUserList()

		case client := <-h.Unregister:
			if _, ok := h.Clients[client.Username]; ok {
				delete(h.Clients, client.Username)
				close(client.Send)
				log.Printf("Client %s unregistered. Total clients: %d", client.Username, len(h.Clients))
				h.broadcastUserList()
			}

		case message := <-h.Broadcast:
			// This channel is for messages that should be broadcasted to all local clients.
			// If Redis is configured, broadcast messages might also originate from Redis.
			for _, client := range h.Clients {
				select {
				case client.Send <- message:
				default:
					log.Printf("Client %s send channel blocked, closing connection.", client.Username)
					close(client.Send)
					delete(h.Clients, client.Username)
				}
			}

		case dm := <-h.DirectMessage:
			// Handle direct messages
			if client, ok := h.Clients[dm.To]; ok {
				msg := Message{
					Type:    "direct",
					From:    dm.From,
					To:      dm.To,
					Content: string(dm.Content), // Convert []byte back to string for JSON
				}
				msgBytes, err := json.Marshal(msg)
				if err != nil {
					log.Printf("Error marshalling direct message for %s: %v", dm.To, err)
					continue
				}
				select {
				case client.Send <- msgBytes:
					log.Printf("Direct message sent from %s to %s", dm.From, dm.To)
				default:
					log.Printf("Client %s direct message send channel blocked, closing connection.", client.Username)
					close(client.Send)
					delete(h.Clients, client.Username)
				}
			} else {
				log.Printf("Recipient %s not found for direct message from %s", dm.To, dm.From)
				// Optionally, send an error message back to dm.From
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
		log.Printf("Error marshalling user list: %v", err)
		return
	}

	for _, client := range h.Clients {
		select {
		case client.Send <- msgBytes:
		default:
			log.Printf("Client %s user list send channel blocked, closing connection.", client.Username)
			close(client.Send)
			delete(h.Clients, client.Username)
		}
	}
}
