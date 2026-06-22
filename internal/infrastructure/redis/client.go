package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// NormalizeKeyPrefix ensures a non-empty prefix ends with ":".
func NormalizeKeyPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return prefix
}

// ConnectURL parses url, creates a client, and verifies connectivity with PING.
func ConnectURL(url string) (*goredis.Client, error) {
	if url == "" {
		return nil, fmt.Errorf("redis: empty url")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}
	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}
	return client, nil
}
