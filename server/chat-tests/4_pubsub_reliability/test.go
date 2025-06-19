package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type PubSubTest struct {
	MessageID    int
	DeliveredTo  int
	Expected     int
	DeliveryTime time.Duration
}

func main() {
	// Docker service configuration
	servers := []struct {
		ServiceName string
		Port        int
	}{
		{"server1", 8080},
		{"server2", 8080},
		{"server3", 8080},
		{"server4", 8080},
		{"server5", 8080},
	}

	numClients := 5
	var tests []PubSubTest
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create log file in mounted volume
	logFile, err := os.Create("/app/test-results/message_delivery.log")
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	// Start receivers on different servers
	deliveryChan := make(chan bool, numClients)
	clients := make([]*websocket.Conn, numClients)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			user := fmt.Sprintf("subscriber_%d", idx)
			server := servers[idx%len(servers)]
			url := fmt.Sprintf("ws://%s:%d/ws?username=%s", server.ServiceName, server.Port, user)

			conn, _, err := dialer.Dial(url, nil)
			if err != nil {
				log.Printf("Failed to connect client %d to %s: %v", idx, url, err)
				return
			}
			clients[idx] = conn
			log.Printf("Client %d connected to %s", idx, url)

			for {
				conn.SetReadDeadline(time.Now().Add(15 * time.Second))
				_, msgBytes, err := conn.ReadMessage()
				if err != nil {
					log.Printf("Client %d read error: %v", idx, err)
					return
				}

				var msg struct {
					TestID string `json:"test_id"`
				}
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					log.Printf("Client %d JSON error: %v", idx, err)
					continue
				}

				if msg.TestID != "" {
					log.Printf("Client %d received message %s", idx, msg.TestID)
					deliveryChan <- true
				}
			}
		}(i)
	}

	// Wait for clients to connect
	time.Sleep(5 * time.Second)

	// Send test messages
	for i := 1; i <= 3; i++ {
		start := time.Now()
		testID := fmt.Sprintf("test-%d", i)
		server := servers[0] // Use first server for sending

		conn, _, err := dialer.Dial(
			fmt.Sprintf("ws://%s:%d/ws?username=sender", server.ServiceName, server.Port), nil)
		if err != nil {
			log.Printf("Failed to create sender for message %d: %v", i, err)
			continue
		}

		msg := map[string]interface{}{
			"from":    "sender",
			"content": fmt.Sprintf("TEST MESSAGE %d", i),
			"test_id": testID,
		}

		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("Failed to send message %d: %v", i, err)
			conn.Close()
			continue
		}
		conn.Close()
		log.Printf("Sent message %s", testID)

		// Collect deliveries
		delivered := 0
		timeout := time.After(10 * time.Second) // Increased timeout
	collectionLoop:
		for delivered < numClients {
			select {
			case <-deliveryChan:
				delivered++
				log.Printf("Message %s delivered to %d/%d", testID, delivered, numClients)
			case <-timeout:
				log.Printf("Timeout waiting for message %s deliveries", testID)
				break collectionLoop
			}
		}

		test := PubSubTest{
			MessageID:    i,
			DeliveredTo:  delivered,
			Expected:     numClients,
			DeliveryTime: time.Since(start),
		}
		mu.Lock()
		tests = append(tests, test)
		mu.Unlock()
	}

	// Close all clients
	for i, conn := range clients {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Printf("Error closing client %d: %v", i, err)
			}
		}
	}

	// Write results
	for _, test := range tests {
		_, err := logFile.WriteString(fmt.Sprintf(
			"Message %d: Delivered to %d/%d, Time: %v\n",
			test.MessageID, test.DeliveredTo, test.Expected, test.DeliveryTime))
		if err != nil {
			log.Printf("Failed to write log entry: %v", err)
		}
	}
}