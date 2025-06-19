package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type PerformanceMetric struct {
	Timestamp         time.Time
	MessagesPerSecond float64 // Changed to float for more precision
	AvgLatencyMs      float64
	SuccessRate       float64
	ClientsConnected  int
	MessagesDropped   int
	ErrorCount        int // New field for error tracking
}

func main() {
	// Docker service configuration
	servers := []struct {
		ServiceName string
		Port        int // Container port (always 8080)
	}{
		{"server1", 8080},
		{"server2", 8080},
		{"server3", 8080},
		{"server4", 8080},
		{"server5", 8080},
	}

	numClients := 100
	var metrics []PerformanceMetric
	var mu sync.Mutex

	var totalSent atomic.Int64
	var totalReceived atomic.Int64
	var totalLatency atomic.Int64
	var connectedClients atomic.Int64
	var errorCount atomic.Int64

	// Configure WebSocket dialer with timeouts
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	// Create results directory if not exists
	if err := os.MkdirAll("/app/test-results", 0755); err != nil {
		log.Fatalf("Failed to create results directory: %v", err)
	}

	// Start clients with connection retries
	for i := 0; i < numClients; i++ {
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond) // Stagger connections

		go func(clientID int) {
			server := servers[rand.Intn(len(servers))]
			user := fmt.Sprintf("stress_%d", clientID)
			url := fmt.Sprintf("ws://%s:%d/ws?username=%s", server.ServiceName, server.Port, user)

			conn, _, err := dialer.Dial(url, nil)
			if err != nil {
				errorCount.Add(1)
				log.Printf("Client %d failed to connect to %s: %v", clientID, url, err)
				return
			}
			connectedClients.Add(1)
			defer func() {
				conn.Close()
				connectedClients.Add(-1)
			}()

			// Receiver with enhanced error handling
			go func() {
				for {
					conn.SetReadDeadline(time.Now().Add(30 * time.Second))
					_, msgBytes, err := conn.ReadMessage()
					if err != nil {
						errorCount.Add(1)
						return
					}

					var msg struct {
						SentAt int64 `json:"sent_at"`
					}
					if err := json.Unmarshal(msgBytes, &msg); err == nil && msg.SentAt > 0 {
						latency := time.Now().UnixNano() - msg.SentAt
						totalLatency.Add(latency)
						totalReceived.Add(1)
					}
				}
			}()

			// Sender with controlled message rate
			for i := 0; i < 100; i++ {
				time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond) // Random delay between messages

				msg := map[string]interface{}{
					"from":    user,
					"content": fmt.Sprintf("Message %d", i),
					"sent_at": time.Now().UnixNano(),
				}

				if err := conn.WriteJSON(msg); err != nil {
					errorCount.Add(1)
					continue
				}
				totalSent.Add(1)
			}
		}(i)
	}

	// Monitor performance with more detailed metrics
	ticker := time.NewTicker(5 * time.Second) // Increased interval for Docker environments
	defer ticker.Stop()
	timeout := time.After(300 * time.Second) // Increased timeout for container startup

	var lastReceived int64
	var lastTimestamp time.Time

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			sent := totalSent.Load()
			received := totalReceived.Load()
			latency := totalLatency.Load()
			clients := connectedClients.Load()
			errors := errorCount.Load()

			var mps float64
			if !lastTimestamp.IsZero() {
				elapsed := now.Sub(lastTimestamp).Seconds()
				mps = float64(received-lastReceived) / elapsed
			}
			lastReceived = received
			lastTimestamp = now

			var avgLatency float64
			if received > 0 {
				avgLatency = float64(latency) / float64(received) / 1e6
			}

			successRate := 0.0
			if sent > 0 {
				successRate = float64(received) / float64(sent) * 100
			}

			mu.Lock()
			metrics = append(metrics, PerformanceMetric{
				Timestamp:         now,
				MessagesPerSecond: mps,
				AvgLatencyMs:      avgLatency,
				SuccessRate:       successRate,
				ClientsConnected:  int(clients),
				MessagesDropped:   int(sent - received),
				ErrorCount:        int(errors),
			})
			mu.Unlock()

			log.Printf("Status: %d clients, %.1f msg/sec, %.2fms latency, %.1f%% success",
				clients, mps, avgLatency, successRate)

		case <-timeout:
			if err := writePerformanceMetrics(metrics); err != nil {
				log.Fatalf("Failed to write metrics: %v", err)
			}
			return
		}
	}
}

func writePerformanceMetrics(metrics []PerformanceMetric) error {
	file, err := os.Create("/app/test-results/performance.csv")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"timestamp", "msg_per_sec", "avg_latency_ms",
		"success_rate_percent", "clients_connected",
		"messages_dropped", "error_count",
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	for _, m := range metrics {
		record := []string{
			m.Timestamp.Format(time.RFC3339),
			fmt.Sprintf("%.1f", m.MessagesPerSecond),
			fmt.Sprintf("%.2f", m.AvgLatencyMs),
			fmt.Sprintf("%.1f", m.SuccessRate),
			fmt.Sprintf("%d", m.ClientsConnected),
			fmt.Sprintf("%d", m.MessagesDropped),
			fmt.Sprintf("%d", m.ErrorCount),
		}
		if err := writer.Write(record); err != nil {
			log.Printf("Failed to write record: %v", err)
			continue
		}
	}

	return nil
}