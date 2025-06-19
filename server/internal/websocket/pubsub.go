package websocket

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type RedisPubSub struct {
	Client *redis.Client
	Hub    *Hub
}

// Retry connecting to Redis in case it's not yet available (e.g. container startup delay)
func connectRedisWithRetry(addr string, maxRetries int) *redis.Client {
	var rdb *redis.Client
	var err error

	for i := 0; i < maxRetries; i++ {
		rdb = redis.NewClient(&redis.Options{
			Addr: addr,
		})

		_, err = rdb.Ping(ctx).Result()
		if err == nil {
			log.Println("✅ Connected to Redis at", addr)
			return rdb
		}

		log.Printf("⏳ Redis connection failed (attempt %d/%d): %v", i+1, maxRetries, err)
		time.Sleep(2 * time.Second)
	}

	log.Fatalf("❌ Could not connect to Redis after %d attempts: %v", maxRetries, err)
	return nil
}

func NewRedisPubSub(hub *Hub) *RedisPubSub {
	rdb := connectRedisWithRetry("redis:6379", 5)

	// Attach Redis client and context to hub
	hub.RedisClient = rdb
	hub.RedisContext = ctx

	pubsub := &RedisPubSub{
		Client: rdb,
		Hub:    hub,
	}

	// Start listening on Redis channel in background
	go pubsub.Subscribe(RedisChannel)

	log.Printf("✅ RedisPubSub initialized and subscribed to channel: %s", RedisChannel)

	return pubsub
}

// Publish sends a message to a Redis channel
func (r *RedisPubSub) Publish(channel string, message []byte) {
	err := r.Client.Publish(ctx, channel, message).Err()
	if err != nil {
		log.Println("❌ Redis publish error:", err)
	}
}

// Subscribe listens to a Redis channel and pushes messages to the local Hub
func (r *RedisPubSub) Subscribe(channel string) {
	sub := r.Client.Subscribe(ctx, channel)
	ch := sub.Channel()

	log.Printf("📡 Redis subscriber listening on channel: %s", channel)

	for msg := range ch {
		log.Printf("📥 Redis message received: %s", msg.Payload)
		r.Hub.Broadcast <- []byte(msg.Payload)
	}
}
