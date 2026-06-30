package cache_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/valpere/ragivka/pkg/cache"
)

// memCache is a minimal in-memory Cache for testing the interface contract.
type memCache struct {
	data map[string]json.RawMessage
}

func newMemCache() cache.Cache {
	return &memCache{data: make(map[string]json.RawMessage)}
}

func (m *memCache) Get(_ context.Context, key string) (json.RawMessage, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, cache.ErrCacheMiss
	}
	return v, nil
}

func (m *memCache) Set(_ context.Context, key string, val json.RawMessage, _ time.Duration) error {
	m.data[key] = val
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCache_SetAndGet(t *testing.T) {
	c := newMemCache()
	ctx := context.Background()
	want := json.RawMessage(`{"result":"ok"}`)

	if err := c.Set(ctx, "k1", want, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCache_GetMissingKeyReturnsErrCacheMiss(t *testing.T) {
	c := newMemCache()
	_, err := c.Get(context.Background(), "nonexistent")
	if !errors.Is(err, cache.ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}

func TestCache_OverwriteKey(t *testing.T) {
	c := newMemCache()
	ctx := context.Background()

	_ = c.Set(ctx, "k", json.RawMessage(`"first"`), 0)
	_ = c.Set(ctx, "k", json.RawMessage(`"second"`), 0)

	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != `"second"` {
		t.Errorf("got %s, want \"second\"", got)
	}
}
