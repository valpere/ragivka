package web

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Broadcaster delivers assistant replies to WebSocket subscribers of a
// session (FR-22 — async L2/L3 delivery). Subscribe returns a channel that
// receives every message Published for sessionID until the returned cancel
// func is called.
type Broadcaster interface {
	Publish(ctx context.Context, sessionID uuid.UUID, msg []byte) error
	Subscribe(ctx context.Context, sessionID uuid.UUID) (ch <-chan []byte, cancel func())
}

// MemoryBroadcaster is an in-process Broadcaster suitable for a single-process
// deployment (per CLAUDE.md: "can run as a single process or split into
// independent API and Worker processes"). A Redis pub/sub-backed
// implementation is required once the API and Worker run as separate
// processes — out of scope here.
type MemoryBroadcaster struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan []byte]struct{}
}

// NewMemoryBroadcaster constructs an empty MemoryBroadcaster.
func NewMemoryBroadcaster() *MemoryBroadcaster {
	return &MemoryBroadcaster{subs: make(map[uuid.UUID]map[chan []byte]struct{})}
}

// Publish delivers msg to all current subscribers of sessionID. Slow
// subscribers are dropped from delivery for this message rather than
// blocking the publisher (buffered channel with non-blocking send).
func (b *MemoryBroadcaster) Publish(_ context.Context, sessionID uuid.UUID, msg []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[sessionID] {
		select {
		case ch <- msg:
		default:
		}
	}
	return nil
}

// Subscribe registers a new listener for sessionID. The returned cancel func
// must be called to release resources when the listener is done.
func (b *MemoryBroadcaster) Subscribe(_ context.Context, sessionID uuid.UUID) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	b.mu.Lock()
	if b.subs[sessionID] == nil {
		b.subs[sessionID] = make(map[chan []byte]struct{})
	}
	b.subs[sessionID][ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[sessionID], ch)
		if len(b.subs[sessionID]) == 0 {
			delete(b.subs, sessionID)
		}
		close(ch)
	}
	return ch, cancel
}
