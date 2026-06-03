package events

import (
	"sync"
)

type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan Event),
	}
}

func (eb *EventBus) Subscribe(bic string, buf int) (<-chan Event, func()) {
	ch := make(chan Event, buf)
	eb.mu.Lock()
	eb.subscribers[bic] = append(eb.subscribers[bic], ch)
	eb.mu.Unlock()

	unsubscribe := func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		subs := eb.subscribers[bic]
		for i, sub := range subs {
			if sub == ch {
				eb.subscribers[bic] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, unsubscribe
}

func (eb *EventBus) Publish(bic string, event Event) {
	eb.mu.RLock()
	subs := eb.subscribers[bic]
	eb.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (eb *EventBus) PublishToAll(bics []string, event Event) {
	for _, bic := range bics {
		eb.Publish(bic, event)
	}
}
