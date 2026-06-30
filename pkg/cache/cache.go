package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrCacheMiss is returned by Get when the key is not present in the cache.
var ErrCacheMiss = errors.New("cache: miss")

// Cache is the read-tool caching abstraction (FR-16).
// Results are stored as JSON; callers supply TTL per tool type.
type Cache interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)
	Set(ctx context.Context, key string, val json.RawMessage, ttl time.Duration) error
}

// RedisCache implements Cache using Redis (FR-16, optional Redis dependency).
type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Get retrieves a cached value. Returns ErrCacheMiss if the key is absent.
func (c *RedisCache) Get(ctx context.Context, key string) (json.RawMessage, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(val), nil
}

// Set stores a value with the given TTL. A zero TTL means no expiry.
func (c *RedisCache) Set(ctx context.Context, key string, val json.RawMessage, ttl time.Duration) error {
	return c.client.Set(ctx, key, []byte(val), ttl).Err()
}
