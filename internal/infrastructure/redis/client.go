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

// ApplyLimiterClientOptions sets short I/O timeouts and disables retries so
// a hung Redis cannot stall HTTP requests. poolSize 0 uses DefaultLimiterPoolSize.
// Only redis:// and rediss:// single-node URLs are supported (not Cluster/Sentinel).
func ApplyLimiterClientOptions(opts *goredis.Options, poolSize int) {
	if opts == nil {
		return
	}
	opts.ContextTimeoutEnabled = true
	opts.DialTimeout = slidingWindowAllow
	opts.ReadTimeout = slidingWindowAllow
	opts.WriteTimeout = slidingWindowAllow
	opts.PoolTimeout = slidingWindowAllow
	// -1 disables retries; 0 is interpreted as the library default (3).
	opts.MaxRetries = -1
	if poolSize <= 0 {
		poolSize = DefaultLimiterPoolSize()
	}
	opts.PoolSize = poolSize
}

// NewLimiterClient parses url and returns a client with limiter I/O options.
// It does not ping. Only single-node redis:// / rediss:// URLs are accepted.
func NewLimiterClient(url string, poolSize int) (*goredis.Client, error) {
	if url == "" {
		return nil, fmt.Errorf("redis: empty url")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}
	ApplyLimiterClientOptions(opts, poolSize)
	return goredis.NewClient(opts), nil
}

// PingLimiter PING the client with timeout.
func PingLimiter(client *goredis.Client, timeout time.Duration) error {
	if client == nil {
		return fmt.Errorf("redis: nil client")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}
