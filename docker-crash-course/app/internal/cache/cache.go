// Package cache wraps the Redis client used for view-count caching.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Connect creates a Redis client and verifies it with a PING.
func Connect(ctx context.Context, addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

// IncrHits bumps the view counter for an item and returns the new total.
func IncrHits(ctx context.Context, client *redis.Client, itemID int64) (int64, error) {
	key := fmt.Sprintf("item:%d:hits", itemID)
	return client.Incr(ctx, key).Result()
}
