package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Stats struct {
	sent       int32
	received   int32
	errors     int32
	latencySum int64 // nanoseconds
}

func main() {
	server := "ws://localhost:8080/ws?username=testuser"

	stats := &Stats{}
	conn, _, err := websocket.DefaultDialer.Dial(server, nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	done := make(chan struct{})

	// Message receiver
	go func() {
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				atomic.AddInt32(&stats.errors, 1)
				close(done)
				return
			}
			atomic.AddInt32(&stats.received, 1)

			var msg struct {
				SentAt int64 `json:"sent_at"`
			}
			if json.Unmarshal(msgBytes, &msg) == nil && msg.SentAt > 0 {
				latency := time.Now().UnixNano() - msg.SentAt
				atomic.AddInt64(&stats.latencySum, latency)
			}
		}
	}()

	// Message sender
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				msg := map[string]interface{}{
					"type":    "message",
					"to":      "all",
					"content": "hello",
					"sent_at": t.UnixNano(),
				}
				msgBytes, _ := json.Marshal(msg)
				err := conn.WriteMessage(websocket.TextMessage, msgBytes)
				if err != nil {
					log.Printf("Failed to send: %v", err)
					atomic.AddInt32(&stats.errors, 1)
					return
				}
				atomic.AddInt32(&stats.sent, 1)
			}
		}
	}()

	// Run test for 30 seconds
	time.Sleep(30 * time.Second)
	close(done)

	printAndSaveStats(stats)
}

func printAndSaveStats(stats *Stats) {
	sent := atomic.LoadInt32(&stats.sent)
	received := atomic.LoadInt32(&stats.received)
	errors := atomic.LoadInt32(&stats.errors)
	latencySum := atomic.LoadInt64(&stats.latencySum)

	avgLatency := time.Duration(0)
	if received > 0 {
		avgLatency = time.Duration(latencySum / int64(received))
	}

	fmt.Println("==== WebSocket Client Test Summary ====")
	fmt.Printf("Messages Sent:     %d\n", sent)
	fmt.Printf("Messages Received: %d\n", received)
	fmt.Printf("Errors:            %d\n", errors)
	fmt.Printf("Avg Latency:       %s\n", avgLatency)

	// Save to CSV
	file, err := os.Create("client_metrics.csv")
	if err != nil {
		log.Printf("Failed to save metrics: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"sent", "received", "errors", "avg_latency_ns"})
	writer.Write([]string{
		fmt.Sprintf("%d", sent),
		fmt.Sprintf("%d", received),
		fmt.Sprintf("%d", errors),
		fmt.Sprintf("%d", avgLatency.Nanoseconds()),
	})

	log.Println("Metrics saved to client_metrics.csv")
}
