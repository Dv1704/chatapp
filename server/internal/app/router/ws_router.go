package router

import (
	"net/http"

	server "github.com/dv1704/chatapp/internal/app/server"
	"github.com/dv1704/chatapp/internal/websocket"
)

// ✅ Accept both hub and pubsub as parameters
func RegisterWebSocketRoutes(hub *websocket.Hub, pubsub *websocket.RedisPubSub) http.Handler {
	mux := http.NewServeMux()

	// Maps the "/ws" URL to the WebSocket handler
	mux.HandleFunc("/ws", WebsocketHandler(hub, pubsub))

	// Maps the "/health" URL to a health check endpoint
	mux.HandleFunc("/health", HealthCheckHandler)

	return mux
}


func WebsocketHandler(hub *websocket.Hub, pubsub *websocket.RedisPubSub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		server.ServeWS(hub, pubsub, w, r)
	}
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ok"))
}
