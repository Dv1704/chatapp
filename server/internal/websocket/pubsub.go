package websocket

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type RedisPubSub struct {
	Client *redis.Client
	Hub    *Hub
}

func NewRedisPubSub(hub *Hub) *RedisPubSub {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	// Assign Redis to Hub so that it can publish
	hub.RedisClient = rdb
	hub.RedisContext = ctx

	return &RedisPubSub{
		Client: rdb,
		Hub:    hub,
	}

}

//publish messasge to redis

func (r *RedisPubSub) Publish(channel string, message []byte) {
	err := r.Client.Publish(ctx, channel, message).Err()
	if err != nil {
		log.Println("Redis publish error: ", err)
	}
}

//subscribe and push to hub

func (r *RedisPubSub) Subscribe(channel string) {
	sub := r.Client.Subscribe(ctx, channel)
	ch := sub.Channel()

	go func() {
		for msg := range ch {
			//redis message comes in as string
			r.Hub.Broadcast <- []byte(msg.Payload)
		}
	}()
}
