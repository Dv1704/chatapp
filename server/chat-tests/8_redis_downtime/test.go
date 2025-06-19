package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type DowntimeMetric struct {
	Timestamp        time.Time
	Phase            string // "pre", "during", "post"
	MessagesSent     int
	MessagesReceived int
	ErrorRate        float64
	RecoveryTime     time.Duration
	RedisStatus      string // Additional Redis status info
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

	redisClient := redis.NewClient(&redis.Options{
		Addr:     "redis:6379", // Using Docker service name
		Password: "",           // no password set
		DB:       0,           // use default DB
	})

	var metrics []DowntimeMetric
	var mu sync.Mutex

	// Create results directory if not exists
	if err := os.MkdirAll("/app/test-results", 0755); err != nil {
		log.Fatalf("Failed to create results directory: %v", err)
	}

	// Phase 1: Pre-downtime baseline
	log.Println("Starting pre-downtime monitoring")
	preDowntime := testMessaging(servers, 30*time.Second, redisClient)
	mu.Lock()
	metrics = append(metrics, DowntimeMetric{
		Timestamp:        time.Now(),
		Phase:            "pre",
		MessagesSent:     preDowntime.sent,
		MessagesReceived: preDowntime.received,
		ErrorRate:        preDowntime.errorRate,
		RedisStatus:      "healthy",
	})
	mu.Unlock()

	// Phase 2: Simulate Redis downtime
	log.Println("Simulating Redis downtime")
	if err := redisClient.Shutdown(context.Background()).Err(); err != nil {
		log.Printf("Failed to shutdown Redis: %v", err)
	}
	time.Sleep(5 * time.Second) // Wait for shutdown to complete

	duringDowntime := testMessaging(servers, 30*time.Second, nil) // Pass nil to indicate Redis is down
	mu.Lock()
	metrics = append(metrics, DowntimeMetric{
		Timestamp:        time.Now(),
		Phase:            "during",
		MessagesSent:     duringDowntime.sent,
		MessagesReceived: duringDowntime.received,
		ErrorRate:        duringDowntime.errorRate,
		RedisStatus:      "down",
	})
	mu.Unlock()

	// Phase 3: Redis recovery
	log.Println("Simulating Redis recovery")
	// In a real Docker environment, you would restart the Redis container here
	// For this test, we'll just reconnect
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "",
		DB:       0,
	})

	recoveryStart := time.Now()
	if err := waitForRedis(redisClient, 30*time.Second); err != nil {
		log.Fatalf("Redis did not recover: %v", err)
	}

	postDowntime := testMessaging(servers, 30*time.Second, redisClient)
	mu.Lock()
	metrics = append(metrics, DowntimeMetric{
		Timestamp:        time.Now(),
		Phase:            "post",
		MessagesSent:     postDowntime.sent,
		MessagesReceived: postDowntime.received,
		ErrorRate:        postDowntime.errorRate,
		RecoveryTime:     time.Since(recoveryStart),
		RedisStatus:      "recovered",
	})
	mu.Unlock()

	if err := writeDowntimeMetrics(metrics); err != nil {
		log.Fatalf("Failed to write metrics: %v", err)
	}
}

type testResult struct {
	sent      int
	received  int
	errorRate float64
}

func testMessaging(servers []struct {
	ServiceName string
	Port        int
}, duration time.Duration, redisClient *redis.Client) testResult {
	var sent, received, errors int
	var wg sync.WaitGroup

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// Start sender
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, _, err := dialer.Dial(
			fmt.Sprintf("ws://%s:%d/ws?username=sender", servers[0].ServiceName, servers[0].Port), nil)
		if err != nil {
			errors++
			return
		}
		defer conn.Close()

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.After(duration)

		for {
			select {
			case <-ticker.C:
				// Check Redis status if available
				if redisClient != nil {
					if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
						errors++
						continue
					}
				}

				msg := map[string]interface{}{
					"from":    "sender",
					"content": "TEST MESSAGE",
					"sent_at": time.Now().UnixNano(),
				}
				if err := conn.WriteJSON(msg); err != nil {
					errors++
					continue
				}
				sent++
			case <-timeout:
				return
			}
		}
	}()

	// Start receiver
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, _, err := dialer.Dial(
			fmt.Sprintf("ws://%s:%d/ws?username=receiver", servers[1].ServiceName, servers[1].Port), nil)
		if err != nil {
			errors++
			return
		}
		defer conn.Close()

		timeout := time.After(duration + 10*time.Second) // Extended timeout for Docker
		for {
			select {
			case <-timeout:
				return
			default:
				conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // Increased read timeout
				_, _, err := conn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err) && !websocket.IsUnexpectedCloseError(err) {
						continue
					}
					return
				}
				received++
			}
		}
	}()

	wg.Wait()

	errorRate := 0.0
	if sent > 0 {
		errorRate = float64(errors) / float64(sent) * 100
	}

	return testResult{
		sent:      sent,
		received:  received,
		errorRate: errorRate,
	}
}

func waitForRedis(client *redis.Client, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if _, err := client.Ping(ctx).Result(); err == nil {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Redis to recover")
		}
	}
}

func writeDowntimeMetrics(metrics []DowntimeMetric) error {
	file, err := os.Create("/app/test-results/downtime_impact.csv")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"timestamp", "phase", "messages_sent",
		"messages_received", "error_rate_percent",
		"recovery_time_ms", "redis_status",
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	for _, m := range metrics {
		record := []string{
			m.Timestamp.Format(time.RFC3339),
			m.Phase,
			fmt.Sprintf("%d", m.MessagesSent),
			fmt.Sprintf("%d", m.MessagesReceived),
			fmt.Sprintf("%.1f", m.ErrorRate),
			fmt.Sprintf("%.0f", m.RecoveryTime.Seconds()*1000),
			m.RedisStatus,
		}
		if err := writer.Write(record); err != nil {
			log.Printf("Failed to write record: %v", err)
			continue
		}
	}

	return nil
}