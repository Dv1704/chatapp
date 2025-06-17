package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/dv1704/chatapp/internal/app/router"
	"github.com/dv1704/chatapp/internal/websocket"
)

func main() {
	// Parse port from command-line arguments
	port := flag.String("port", "8080", "Port to run the server on")
	flag.Parse()

	// Initialize the WebSocket hub
	hub := websocket.NewHub()

	// Initialize Redis pub/sub and assign to hub
	pubsub := websocket.NewRedisPubSub(hub)
	hub.RedisClient = pubsub.Client
	hub.RedisContext = context.Background()

	go hub.Run()
	go pubsub.Subscribe("Chat-broadcast")

	// Pass both hub and pubsub into router
	wsHandler := router.RegisterWebSocketRoutes(hub, pubsub)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("✅ Server started at port: %s\n", *port)

	// Start HTTP server
	err := http.ListenAndServe(addr, wsHandler)
	if err != nil {
		log.Fatalf("❌ ListenAndServe: %v", err)
	}
}
