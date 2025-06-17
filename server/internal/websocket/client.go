package websocket

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const RedisChannel = "chat-broadcast"

type Client struct {
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *Hub
	Username     string
	MessageCount int
	LimitReset   *time.Ticker
	RedisPubSub  *RedisPubSub
}

// ReadPump reads messages from the WebSocket connection
func (c *Client) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
		if c.LimitReset != nil {
			c.LimitReset.Stop()
		}
		log.Printf("🔌 Client %s disconnected and cleaned up", c.Username)
	}()

	// Set read limits and pong handler
	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		return nil
	})

	// Rate limiter reset ticker
	if c.LimitReset == nil {
		c.LimitReset = time.NewTicker(1 * time.Minute)
	}
	go func() {
		for range c.LimitReset.C {
			c.MessageCount = 0
		}
	}()

	for {
		_, msgBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️ Unexpected close from %s: %v", c.Username, err)
			} else {
				log.Printf("ℹ️ Closing connection for %s: %v", c.Username, err)
			}
			break
		}

		// Rate limiting
		if c.MessageCount >= 10 {
			c.Send <- []byte("⚠️ Rate limit exceeded. Please slow down.")
			continue
		}
		c.MessageCount++

		var msg Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			log.Println("❌ Invalid message format:", err)
			c.Send <- []byte("❌ Error: Invalid message format.")
			continue
		}

		msg.From = c.Username

		contentStr, ok := msg.Content.(string)
		if !ok {
			c.Send <- []byte("❌ Error: Message content must be a string.")
			continue
		}
		msg.Content = contentStr

		log.Printf("💬 Message from %s to %s: %s", msg.From, msg.To, contentStr)

		if msg.To == "" {
			// Broadcast to all users via Redis or fallback
			jsonMsg, err := json.Marshal(msg)
			if err != nil {
				log.Println("❌ Error marshalling message:", err)
				continue
			}

			if hub.RedisClient != nil && hub.RedisContext != nil {
				err := hub.RedisClient.Publish(hub.RedisContext, RedisChannel, jsonMsg).Err()
				if err != nil {
					log.Println("❌ Redis publish error:", err)
				} else {
					log.Println("📡 Broadcast message published to Redis.")
				}
			} else {
				log.Println("⚠️ Redis unavailable. Using in-memory broadcast.")
				hub.Broadcast <- jsonMsg
			}
		} else {
			hub.DirectMessage <- DirectMessage{
				To:      msg.To,
				From:    msg.From,
				Content: []byte(contentStr),
			}
		}
	}
}

// WritePump writes messages to the WebSocket connection
func (c *Client) WritePump() {
	defer func() {
		c.Conn.Close()
	}()

	ticker := time.NewTicker(50 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Hub closed the channel
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("❌ Error sending message to %s: %v", c.Username, err)
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("❌ Ping failed for %s: %v", c.Username, err)
				return
			}
		}
	}
}
