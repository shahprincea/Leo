package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// New creates a new Redis client and verifies connectivity via Ping.
func New(ctx context.Context, redisURL string) (*redis.Client, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("cache: REDIS_URL is not set")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("cache: invalid REDIS_URL: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cache: unable to reach Redis: %w", err)
	}

	return client, nil
}
