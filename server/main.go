package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/dv1704/chatapp/internal/app/router"
	"github.com/dv1704/chatapp/internal/websocket"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Parse port from command-line arguments
	port := flag.String("port", "8080", "Port to run the server on")
	flag.Parse()

	// ✅ Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("❌ Redis connection failed: %v", err)
	}

	// ✅ Pass redisClient into NewHub
	hub := websocket.NewHub(*port, redisClient)
	go hub.Run()

	// Redis pub/sub
	pubsub := websocket.NewRedisPubSub(hub)

	// Register WebSocket routes
	wsHandler := router.RegisterWebSocketRoutes(hub, pubsub)

	// Start HTTP server
	addr := fmt.Sprintf(":%s", *port)
	log.Printf("✅ Server started at port: %s\n", *port)

	if err := http.ListenAndServe(addr, wsHandler); err != nil {
		log.Fatalf("❌ ListenAndServe: %v", err)
	}
}
