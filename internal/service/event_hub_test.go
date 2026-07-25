package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func receiveEvent(t *testing.T, events <-chan AppEvent) (AppEvent, bool) {
	t.Helper()
	select {
	case event, ok := <-events:
		return event, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return AppEvent{}, false
	}
}

func TestEventHubPublishesToTwoSubscribers(t *testing.T) {
	hub := NewEventHub(8, 2)
	defer hub.Close()
	first, unsubscribeFirst := hub.Subscribe()
	defer unsubscribeFirst()
	second, unsubscribeSecond := hub.Subscribe()
	defer unsubscribeSecond()

	require.True(t, hub.Publish(AppEvent{Type: "RoomCreated", Data: "room-1"}))
	firstEvent, ok := receiveEvent(t, first)
	require.True(t, ok)
	secondEvent, ok := receiveEvent(t, second)
	require.True(t, ok)
	require.Equal(t, "RoomCreated", firstEvent.Type)
	require.Equal(t, firstEvent, secondEvent)
}

func TestEventHubUnsubscribeRemovesExactlyOneSubscriber(t *testing.T) {
	hub := NewEventHub(8, 2)
	defer hub.Close()
	first, unsubscribeFirst := hub.Subscribe()
	second, unsubscribeSecond := hub.Subscribe()
	defer unsubscribeSecond()
	unsubscribeFirst()
	unsubscribeFirst()

	require.True(t, hub.Publish(AppEvent{Type: "RoomDeleted"}))
	event, ok := receiveEvent(t, second)
	require.True(t, ok)
	require.Equal(t, "RoomDeleted", event.Type)
	select {
	case _, open := <-first:
		require.False(t, open)
	case <-time.After(time.Second):
		t.Fatal("unsubscribed channel was not closed")
	}
}

func TestEventHubSlowSubscriberDoesNotBlockAndCountsDrops(t *testing.T) {
	hub := NewEventHub(32, 1)
	defer hub.Close()
	_, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	start := time.Now()
	for index := 0; index < 20; index++ {
		hub.Publish(AppEvent{Type: "RoomUpdated", Data: index})
	}
	require.Less(t, time.Since(start), 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return hub.Dropped() > 0
	}, time.Second, time.Millisecond)
}

func TestEventHubCloseClosesSubscriptionsAndPublishIsSafe(t *testing.T) {
	hub := NewEventHub(2, 2)
	events, unsubscribe := hub.Subscribe()
	hub.Close()
	hub.Close()
	unsubscribe()

	_, ok := receiveEvent(t, events)
	require.False(t, ok)
	require.False(t, hub.Publish(AppEvent{Type: "ignored"}))
}
