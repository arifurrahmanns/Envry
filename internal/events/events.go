// Package events is a small in-process publish/subscribe bus. The store
// publishes changes; the WebSocket hub, /wait and IMAP IDLE subscribe.
package events

import (
	"sync"
	"sync/atomic"
)

// Event types published on the bus.
const (
	MessageCreated  = "message.created"
	MessageDeleted  = "message.deleted"
	MessagesCleared = "messages.cleared"
	DeliveryUpdated = "delivery.updated"
	DeliveryDeleted = "delivery.deleted"
	MailboxCreated  = "mailbox.created"
	MailboxUpdated  = "mailbox.updated"
)

// Event is one change notification. Data is JSON-serialisable.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Bus fans events out to subscribers. The zero value is not usable; call New.
type Bus struct {
	mu      sync.RWMutex
	subs    map[uint64]chan Event
	nextID  uint64
	dropped atomic.Uint64
}

// New returns an empty bus.
func New() *Bus {
	return &Bus{subs: make(map[uint64]chan Event)}
}

// Subscribe returns a channel of future events and a function that
// unsubscribes and closes the channel. A slow subscriber whose buffer is full
// misses events rather than blocking publishers.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// Publish delivers e to every subscriber without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			b.dropped.Add(1)
		}
	}
}

// Dropped reports how many deliveries were skipped because a subscriber's
// buffer was full.
func (b *Bus) Dropped() uint64 { return b.dropped.Load() }
