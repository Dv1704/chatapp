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

type BroadcastMetric struct {
	Sender        string
	Receivers     int
	AvgLatency    float64
	MinLatency    float64
	MaxLatency    float64
	DeliveryRatio float64
}

func main() {
	
	serverConfigs := []struct {
		ServiceName string
		Port        int
	}{
		{"server1", 8080}, // Container port (not host-mapped port)
		{"server2", 8080},
		{"server3", 8080},
		{"server4", 8080},
		{"server5", 8080},
	}

	users := []string{"user001", "user002", "user003", "user004", "user005"}
	var metrics []BroadcastMetric
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Start receivers
	results := make(chan float64, len(users)-1)
	for _, user := range users[1:] {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			conn := connectWithRetry(u, serverConfigs, 3)
			if conn == nil {
				log.Printf("Receiver %s failed to connect after retries", u)
				return
			}
			defer conn.Close()

			start := time.Now()
			for {
				conn.SetReadDeadline(start.Add(15 * time.Second)) // Increased timeout
				_, msgBytes, err := conn.ReadMessage()
				if err != nil {
					log.Printf("Receiver %s read error: %v", u, err)
					return
				}

				var msg struct {
					From    string `json:"from"`
					SentAt  int64  `json:"sent_at"`
					Content string `json:"content"`
				}
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					log.Printf("Receiver %s JSON error: %v", u, err)
					continue
				}

				if msg.From == users[0] {
					latency := float64(time.Now().UnixNano()-msg.SentAt) / 1e6
					log.Printf("Receiver %s got message with latency %.2fms", u, latency)
					results <- latency
					return
				}
			}
		}(user)
	}

	// Start sender with retries
	time.Sleep(3 * time.Second) // Give receivers more time to connect
	conn := connectWithRetry(users[0], serverConfigs, 5)
	if conn == nil {
		log.Fatal("Sender failed to connect after retries")
	}
	defer conn.Close()

	msg := map[string]interface{}{
		"from":    users[0],
		"content": "BROADCAST TEST",
		"sent_at": time.Now().UnixNano(),
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Fatalf("Failed to send broadcast message: %v", err)
	}
	log.Printf("Sender %s sent broadcast message", users[0])

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	var latencies []float64
	for lat := range results {
		latencies = append(latencies, lat)
	}

	// Calculate metrics
	if len(latencies) > 0 {
		sum, min, max := 0.0, latencies[0], latencies[0]
		for _, lat := range latencies {
			sum += lat
			if lat < min {
				min = lat
			}
			if lat > max {
				max = lat
			}
		}

		mu.Lock()
		metrics = append(metrics, BroadcastMetric{
			Sender:        users[0],
			Receivers:     len(latencies),
			AvgLatency:    sum / float64(len(latencies)),
			MinLatency:    min,
			MaxLatency:    max,
			DeliveryRatio: float64(len(latencies)) / float64(len(users)-1),
		})
		mu.Unlock()
	}

	if err := writeBroadcastMetrics(metrics); err != nil {
		log.Fatalf("Failed to write metrics: %v", err)
	}
}

func connectWithRetry(username string, servers []struct {
	ServiceName string
	Port        int
}, maxAttempts int) *websocket.Conn {
	var conn *websocket.Conn
	var err error

	for i := 0; i < maxAttempts; i++ {
		server := servers[i%len(servers)]
		url := fmt.Sprintf("ws://%s:%d/ws?username=%s", server.ServiceName, server.Port, username)
		
		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			log.Printf("Connected %s to %s", username, url)
			return conn
		}
		
		log.Printf("Attempt %d/%d for %s: %v", i+1, maxAttempts, username, err)
		time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
	}
	return nil
}

func writeBroadcastMetrics(metrics []BroadcastMetric) error {
	file, err := os.Create("/app/test-results/broadcast_metrics.csv")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"sender", "receivers", "avg_latency_ms",
		"min_latency_ms", "max_latency_ms", "delivery_ratio",
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	for _, m := range metrics {
		record := []string{
			m.Sender,
			fmt.Sprintf("%d", m.Receivers),
			fmt.Sprintf("%.2f", m.AvgLatency),
			fmt.Sprintf("%.2f", m.MinLatency),
			fmt.Sprintf("%.2f", m.MaxLatency),
			fmt.Sprintf("%.2f", m.DeliveryRatio),
		}
		if err := writer.Write(record); err != nil {
			log.Printf("Failed to write record: %v", err)
			continue
		}
	}

	return nil
}