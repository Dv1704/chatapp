package main

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	From    string `json:"from"`
	To      string `json:"to,omitempty"`
	Content string `json:"content"`
}

func simulateClient(username string) {
	url := "ws://localhost:8080/ws?username=" + username
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("❌ [%s] Connection failed: %v", username, err)
	}
	defer conn.Close()
	log.Printf("✅ [%s] Connected", username)

	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("⚠️ [%s] Read error: %v", username, err)
				return
			}
			log.Printf("📥 [%s] Received: %s", username, string(msg))
		}
	}()

	if username == "alice" {
		msg := Message{
			From:    username,
			Content: "Hello Bob!",
		}
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("❌ [%s] Send error: %v", username, err)
		} else {
			log.Printf("📤 [%s] Sent broadcast", username)
		}
	}

	time.Sleep(10 * time.Second)
	log.Printf("❌ [%s] Done", username)
}

func main() {
	go simulateClient("bob")
	time.Sleep(1 * time.Second) // wait so bob is ready to receive
	go simulateClient("alice")

	time.Sleep(15 * time.Second)
}
