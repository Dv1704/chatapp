package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	From    string `json:"from"`
	To      string `json:"to,omitempty"`
	Content string `json:"content"`
	SentAt  int64  `json:"sent_at"`
}

type Metric struct {
	Sender       string
	Receiver     string
	DeliveryTime float64 // ms
	Success      bool
}

func main() {
	// Updated ports to match your Docker Compose setup
	serverPorts := []int{8081, 8082, 8083, 8084, 8085} // Docker mapped ports
	var metrics []Metric
	var mu sync.Mutex

	// Test pair
	sender := "user001"
	receiver := "user002"

	// Start receiver first
	go func() {
		conn := connectWithRetry(receiver, serverPorts, 3)
		if conn == nil {
			log.Fatal("Receiver failed to connect after retries")
		}
		defer conn.Close()

		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Receiver read error: %v", err)
				return
			}

			var msg Message
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				log.Printf("Receiver JSON unmarshal error: %v", err)
				continue
			}

			if msg.From == sender {
				latency := float64(time.Now().UnixNano()-msg.SentAt) / 1e6
				mu.Lock()
				metrics = append(metrics, Metric{
					Sender:       sender,
					Receiver:     receiver,
					DeliveryTime: latency,
					Success:      true,
				})
				mu.Unlock()
				return
			}
		}
	}()

	// Sender
	time.Sleep(2 * time.Second)
	conn := connectWithRetry(sender, serverPorts, 3)
	if conn == nil {
		log.Fatal("Sender failed to connect after retries")
	}
	defer conn.Close()

	msg := Message{
		From:    sender,
		To:      receiver,
		Content: "TEST MESSAGE",
		SentAt:  time.Now().UnixNano(),
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	time.Sleep(5 * time.Second)
	writeMetricsToCSV(metrics)
}

func connectWithRetry(user string, ports []int, maxAttempts int) *websocket.Conn {
	var conn *websocket.Conn
	var err error

	for i := 0; i < maxAttempts; i++ {
		port := ports[i%len(ports)] // Round-robin through ports

		
		serviceName := fmt.Sprintf("server%d", port-8080)
		url := fmt.Sprintf("ws://%s:8080/ws?username=%s", serviceName, user)

		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			log.Printf("Connected %s to %s", user, url)
			return conn
		}

		log.Printf("Attempt %d/%d: Failed to connect %s to %s: %v", i+1, maxAttempts, user, url, err)
		time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
	}
	return nil
}

func writeMetricsToCSV(metrics []Metric) {
	file, err := os.Create("/app/test-results/metrics.csv") // Using mounted volume
	if err != nil {
		log.Fatalf("Failed to create metrics file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"sender", "receiver", "latency_ms", "success"}); err != nil {
		log.Fatalf("Failed to write header: %v", err)
	}

	for _, m := range metrics {
		if err := writer.Write([]string{
			m.Sender,
			m.Receiver,
			fmt.Sprintf("%.2f", m.DeliveryTime),
			fmt.Sprintf("%t", m.Success),
		}); err != nil {
			log.Printf("Failed to write metric: %v", err)
		}
	}
}
