package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type FailureMetric struct {
	Timestamp      time.Time
	FailedServer   string
	ActiveClients  int
	MessagesLost   int
	RecoveryTime   time.Duration
	ServiceDegrade float64
	State          string
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

	var metrics []FailureMetric
	var mu sync.Mutex
	var activeClients int32

	if err := os.MkdirAll("/app/test-results", 0755); err != nil {
		log.Fatalf("Failed to create results directory: %v", err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	clients := make(map[string]*websocket.Conn)
	for _, server := range servers {
		conn, _, err := dialer.Dial(
			fmt.Sprintf("ws://%s:%d/ws?username=monitor", server.ServiceName, server.Port), nil)
		if err != nil {
			log.Printf("Failed to connect monitor to %s: %v", server.ServiceName, err)
			continue
		}
		clients[server.ServiceName] = conn
		defer conn.Close()

		go func(s string, c *websocket.Conn) {
			for {
				c.SetReadDeadline(time.Now().Add(15 * time.Second))
				_, _, err := c.ReadMessage()
				if err != nil {
					mu.Lock()
					metrics = append(metrics, FailureMetric{
						Timestamp:    time.Now(),
						FailedServer: s,
						State:        "failure-detected",
					})
					mu.Unlock()
					return
				}
				atomic.AddInt32(&activeClients, 1)
			}
		}(server.ServiceName, conn)
	}

	recordMetric(&mu, &metrics, "initial", "", 0, &activeClients)

	time.Sleep(15 * time.Second)
	failedServer := servers[2].ServiceName // server3
	log.Printf("Simulating failure of %s", failedServer)

	recordMetric(&mu, &metrics, "pre-failure", failedServer, 0, &activeClients)

	if conn, ok := clients[failedServer]; ok {
		conn.Close()
	}

	start := time.Now()
	for i := 0; i < 6; i++ {
		time.Sleep(5 * time.Second)
		degrade := float64(i) * 15.0
		if degrade > 100 {
			degrade = 100
		}
		recordMetric(&mu, &metrics, "failure", failedServer, degrade, &activeClients)
	}

	recordMetric(&mu, &metrics, "recovery-start", failedServer, 100, &activeClients)

	time.Sleep(5 * time.Second)
	recoveryTime := time.Since(start)

	recordMetric(&mu, &metrics, "recovery-complete", failedServer, 0, &activeClients)

	mu.Lock()
	for i := range metrics {
		if metrics[i].State == "recovery-complete" {
			metrics[i].RecoveryTime = recoveryTime
		}
	}
	mu.Unlock()

	if err := writeFailureMetrics(metrics); err != nil {
		log.Fatalf("Failed to write metrics: %v", err)
	}
}

func recordMetric(mu *sync.Mutex, metrics *[]FailureMetric, state string, server string, degrade float64, activeClients *int32) {
	mu.Lock()
	defer mu.Unlock()

	*metrics = append(*metrics, FailureMetric{
		Timestamp:      time.Now(),
		FailedServer:   server,
		ActiveClients:  int(atomic.LoadInt32(activeClients)),
		ServiceDegrade: degrade,
		State:          state,
	})
}

func writeFailureMetrics(metrics []FailureMetric) error {
	file, err := os.Create("/app/test-results/failure_metrics.csv")
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"timestamp", "failed_server", "active_clients",
		"messages_lost", "recovery_time_ms",
		"service_degrade_percent", "state",
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("failed to write headers: %w", err)
	}

	for _, m := range metrics {
		record := []string{
			m.Timestamp.Format(time.RFC3339),
			m.FailedServer,
			fmt.Sprintf("%d", m.ActiveClients),
			fmt.Sprintf("%d", m.MessagesLost),
			fmt.Sprintf("%.0f", m.RecoveryTime.Seconds()*1000),
			fmt.Sprintf("%.1f", m.ServiceDegrade),
			m.State,
		}
		if err := writer.Write(record); err != nil {
			log.Printf("Failed to write record: %v", err)
			continue
		}
	}

	return nil
}
