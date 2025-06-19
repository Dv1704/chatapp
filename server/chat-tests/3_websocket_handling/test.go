package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ConnectionTest struct {
	Username  string
	Success   bool
	Duration  time.Duration
	Error     string
	Server    string 
	Reconnect bool
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

	users := []string{"user001", "user002", "user003"}
	var tests []ConnectionTest
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create log file in mounted volume
	logFile, err := os.Create("/app/test-results/connections.log")
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	// Test normal connections
	for _, user := range users {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			start := time.Now()
			conn, _, err := connectWithRetry(u, servers, 3)
			test := ConnectionTest{
				Username: u,
				Server:   servers[0].ServiceName,
			}

			if err != nil {
				test.Success = false
				test.Error = err.Error()
				log.Printf("Connection failed for %s: %v", u, err)
			} else {
				test.Success = true
				test.Duration = time.Since(start)
				conn.Close()
				log.Printf("Successfully connected %s to %s in %v", u, test.Server, test.Duration)
			}

			mu.Lock()
			tests = append(tests, test)
			mu.Unlock()
		}(user)
	}

	// Test reconnection
	wg.Add(1)
	go func() {
		defer wg.Done()
		u := "user_reconnect"
		
		// Initial connection
		conn, _, err := connectWithRetry(u, servers[:1], 3)
		if err != nil {
			logFile.WriteString(fmt.Sprintf("Initial connection failed for %s: %v\n", u, err))
			return
		}

		// Simulate disconnect after 2 seconds
		time.Sleep(2 * time.Second)
		conn.Close()

		// Reconnect to a different server
		start := time.Now()
		conn, _, err = connectWithRetry(u, servers[1:2], 3) // Connect to server2 only
		test := ConnectionTest{
			Username:  u,
			Server:    servers[1].ServiceName,
			Reconnect: true,
		}

		if err != nil {
			test.Success = false
			test.Error = err.Error()
			log.Printf("Reconnection failed for %s: %v", u, err)
		} else {
			test.Success = true
			test.Duration = time.Since(start)
			conn.Close()
			log.Printf("Successfully reconnected %s to %s in %v", u, test.Server, test.Duration)
		}

		mu.Lock()
		tests = append(tests, test)
		mu.Unlock()
	}()

	wg.Wait()

	// Write results to log file
	for _, test := range tests {
		_, err := logFile.WriteString(fmt.Sprintf(
			"User: %s, Server: %s, Success: %t, Duration: %v, Error: %s, Reconnect: %t\n",
			test.Username, test.Server, test.Success, test.Duration, test.Error, test.Reconnect))
		if err != nil {
			log.Printf("Failed to write log entry: %v", err)
		}
	}
}

func connectWithRetry(username string, servers []struct {
	ServiceName string
	Port        int
}, maxAttempts int) (*websocket.Conn, *websocket.Dialer, error) {
	var conn *websocket.Conn
	var dialer = websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	for i := 0; i < maxAttempts; i++ {
		server := servers[i%len(servers)]
		url := fmt.Sprintf("ws://%s:%d/ws?username=%s", server.ServiceName, server.Port, username)
		
		var err error
		conn, _, err = dialer.Dial(url, nil)
		if err == nil {
			return conn, &dialer, nil
		}
		
		log.Printf("Attempt %d/%d for %s to %s failed: %v", i+1, maxAttempts, username, url, err)
		time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
	}
	return nil, &dialer, fmt.Errorf("failed to connect after %d attempts", maxAttempts)
}