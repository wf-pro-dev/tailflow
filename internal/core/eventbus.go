package core

import (
	"context"
	"strings"
	"sync"
)

type Topic string

const (
	TopicNode     Topic = "node"
	TopicSnapshot Topic = "snapshot"
	TopicEdge     Topic = "edge"
	TopicPort     Topic = "port"
	TopicProxy    Topic = "proxy"
)

const subscriberBufferSize = 16

// EventBus provides in-process topic-based pub/sub fan-out.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[Topic]map[chan any]struct{}
}

// NewEventBus returns a ready-to-use event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[Topic]map[chan any]struct{}),
	}
}

// Publish fans out an event to all subscribers of a topic.
func (b *EventBus) Publish(topic Topic, event any) {
	b.mu.RLock()
	subscribers := make([]chan any, 0, len(b.subscribers[topic]))
	for ch := range b.subscribers[topic] {
		subscribers = append(subscribers, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Slow subscribers should not block the collector or resolver.
		}
	}
}

// Subscribe registers a subscriber for a topic until the context is canceled.
func (b *EventBus) Subscribe(ctx context.Context, topic Topic) <-chan any {
	ch := make(chan any, subscriberBufferSize)

	b.mu.Lock()
	if _, ok := b.subscribers[topic]; !ok {
		b.subscribers[topic] = make(map[chan any]struct{})
	}
	b.subscribers[topic][ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if subscribers, ok := b.subscribers[topic]; ok {
			delete(subscribers, ch)
			if len(subscribers) == 0 {
				delete(b.subscribers, topic)
			}
		}
		b.mu.Unlock()
		close(ch)
	}()

	return ch
}

// BroadcastEvent wraps data in the shared event envelope and publishes it.
func BroadcastEvent[T any](bus *EventBus, name string, data T) {
	if bus == nil {
		return
	}

	bus.Publish(topicForEventName(name), Event[T]{
		Name: name,
		TS:   NowTimestamp(),
		Data: data,
	})
}

func topicForEventName(name string) Topic {
	switch {
	case strings.HasPrefix(name, "node."):
		return TopicNode
	case strings.HasPrefix(name, "snapshot."):
		return TopicSnapshot
	case strings.HasPrefix(name, "edge."), strings.HasPrefix(name, "topology."):
		return TopicEdge
	case strings.HasPrefix(name, "port."):
		return TopicPort
	case strings.HasPrefix(name, "proxy."):
		return TopicProxy
	default:
		return Topic(strings.SplitN(name, ".", 2)[0])
	}
}
