package service

import (
	"sync"
	"sync/atomic"

	"qr-command-center/internal/metrics"
)

// AppEvent is the common room and snapshot event envelope. Event names and
// payload shapes are preserved by the WebSocket adapter.
type AppEvent struct {
	Type string
	Data any
}

type eventSubscription struct {
	id     uint64
	events chan AppEvent
}

type subscribeEventHub struct {
	result chan eventSubscription
}

type unsubscribeEventHub struct {
	id   uint64
	done chan struct{}
}

// EventHub is a bounded, non-blocking in-process fan-out. One owner goroutine
// exclusively mutates the subscriber map. Publish returns false after close or
// when its bounded ingress buffer is full.
type EventHub struct {
	publishCh       chan AppEvent
	operations      chan any
	done            chan struct{}
	closeOnce       sync.Once
	closed          atomic.Bool
	dropped         atomic.Uint64
	subscriberDepth int
}

func NewEventHub(publishDepth, subscriberDepth int) *EventHub {
	if publishDepth <= 0 {
		publishDepth = 100
	}
	if subscriberDepth <= 0 {
		subscriberDepth = 256
	}
	hub := &EventHub{
		publishCh:       make(chan AppEvent, publishDepth),
		operations:      make(chan any),
		done:            make(chan struct{}),
		subscriberDepth: subscriberDepth,
	}
	go hub.run()
	return hub
}

func (h *EventHub) run() {
	subscribers := make(map[uint64]chan AppEvent)
	var nextID uint64
	for {
		select {
		case event := <-h.publishCh:
			for _, subscriber := range subscribers {
				select {
				case subscriber <- event:
				default:
					h.dropped.Add(1)
					if event.Type == "SnapshotCommitted" {
						metrics.WarwickSnapshotWebsocketDropsTotal.Inc()
					}
				}
			}
		case operation := <-h.operations:
			switch request := operation.(type) {
			case subscribeEventHub:
				nextID++
				subscription := eventSubscription{
					id:     nextID,
					events: make(chan AppEvent, h.subscriberDepth),
				}
				subscribers[subscription.id] = subscription.events
				request.result <- subscription
			case unsubscribeEventHub:
				if subscriber, ok := subscribers[request.id]; ok {
					delete(subscribers, request.id)
					close(subscriber)
				}
				close(request.done)
			}
		case <-h.done:
			for id, subscriber := range subscribers {
				delete(subscribers, id)
				close(subscriber)
			}
			return
		}
	}
}

func (h *EventHub) Subscribe() (<-chan AppEvent, func()) {
	if h == nil || h.closed.Load() {
		closed := make(chan AppEvent)
		close(closed)
		return closed, func() {}
	}
	request := subscribeEventHub{result: make(chan eventSubscription, 1)}
	select {
	case h.operations <- request:
	case <-h.done:
		closed := make(chan AppEvent)
		close(closed)
		return closed, func() {}
	}
	var subscription eventSubscription
	select {
	case subscription = <-request.result:
	case <-h.done:
		closed := make(chan AppEvent)
		close(closed)
		return closed, func() {}
	}

	var unsubscribeOnce sync.Once
	return subscription.events, func() {
		unsubscribeOnce.Do(func() {
			request := unsubscribeEventHub{
				id:   subscription.id,
				done: make(chan struct{}),
			}
			select {
			case h.operations <- request:
				select {
				case <-request.done:
				case <-h.done:
				}
			case <-h.done:
			}
		})
	}
}

func (h *EventHub) Publish(event AppEvent) bool {
	if h == nil || h.closed.Load() {
		return false
	}
	select {
	case h.publishCh <- event:
		return true
	default:
		h.dropped.Add(1)
		if event.Type == "SnapshotCommitted" {
			metrics.WarwickSnapshotWebsocketDropsTotal.Inc()
		}
		return false
	}
}

func (h *EventHub) Dropped() uint64 {
	if h == nil {
		return 0
	}
	return h.dropped.Load()
}

func (h *EventHub) Close() {
	if h == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.closed.Store(true)
		close(h.done)
	})
}
