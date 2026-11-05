package events

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	a, cancelA := b.Subscribe(4)
	c, cancelC := b.Subscribe(4)
	defer cancelC()

	b.Publish(Event{Type: MessageCreated, Data: "x"})
	for _, ch := range []<-chan Event{a, c} {
		select {
		case e := <-ch:
			if e.Type != MessageCreated || e.Data != "x" {
				t.Fatalf("got %+v", e)
			}
		case <-time.After(time.Second):
			t.Fatal("event not delivered")
		}
	}

	cancelA()
	cancelA() // idempotent
	if _, ok := <-a; ok {
		t.Fatal("channel not closed after cancel")
	}
	b.Publish(Event{Type: MessageDeleted}) // must not panic on the closed subscriber
	if e := <-c; e.Type != MessageDeleted {
		t.Fatalf("got %+v", e)
	}
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := New()
	_, cancel := b.Subscribe(1)
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(Event{Type: MessageCreated})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
	if b.Dropped() != 9 {
		t.Errorf("Dropped() = %d, want 9", b.Dropped())
	}
}

func TestConcurrentUse(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, cancel := b.Subscribe(8)
			cancel()
		}()
		go func() {
			defer wg.Done()
			b.Publish(Event{Type: MessageCreated})
		}()
	}
	wg.Wait()
}
