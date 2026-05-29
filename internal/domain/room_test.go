package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateNextFetchDelay(t *testing.T) {
	assert.Equal(t, uint64(45), CalculateNextFetchDelay(60))
	assert.Equal(t, uint64(75), CalculateNextFetchDelay(100))
	assert.Equal(t, uint64(90), CalculateNextFetchDelay(120))
}

func TestValidTransitions(t *testing.T) {
	cases := []struct {
		from, to RoomStatus
	}{
		{Idle, Running}, {Idle, Stopped},
		{Running, Fetching}, {Running, Stopped},
		{Fetching, Running}, {Fetching, Warning}, {Fetching, AuthExpired}, {Fetching, Stopped},
		{Warning, Fetching}, {Warning, Stopped},
		{AuthExpired, Stopped},
		{Stopped, Running},
	}
	for _, c := range cases {
		assert.NoError(t, c.from.CanTransitionTo(c.to),
			"expected valid: %s -> %s", c.from, c.to)
	}
}

func TestInvalidTransitions(t *testing.T) {
	cases := []struct {
		from, to RoomStatus
	}{
		{Idle, Idle}, {Idle, Fetching}, {Idle, Warning}, {Idle, AuthExpired},
		{Running, Running}, {Running, Idle}, {Running, Warning}, {Running, AuthExpired},
		{Fetching, Fetching}, {Fetching, Idle},
		{Warning, Warning}, {Warning, Idle}, {Warning, Running}, {Warning, AuthExpired},
		{AuthExpired, AuthExpired}, {AuthExpired, Idle}, {AuthExpired, Running},
		{AuthExpired, Fetching}, {AuthExpired, Warning},
		{Stopped, Stopped}, {Stopped, Idle}, {Stopped, Fetching}, {Stopped, Warning}, {Stopped, AuthExpired},
	}
	for _, c := range cases {
		assert.Error(t, c.from.CanTransitionTo(c.to),
			"expected invalid: %s -> %s", c.from, c.to)
	}
}

func TestTransitionErrorMessage(t *testing.T) {
	err := Idle.CanTransitionTo(Fetching)
	assert.Contains(t, err.Error(), "Idle")
	assert.Contains(t, err.Error(), "Fetching")
}

func TestFetchErrorToRoomStatus(t *testing.T) {
	assert.Equal(t, AuthExpired, ErrAuthExpired.ToRoomStatus())
	assert.Equal(t, Warning, NewNetworkError("timeout").ToRoomStatus())
	assert.Equal(t, Warning, NewInvalidPayloadError("bad json").ToRoomStatus())
}

func TestRoomTransitionTo(t *testing.T) {
	room := NewRoom("c1", nil)
	assert.Equal(t, Idle, room.Status)
	room.TransitionTo(Running)
	assert.Equal(t, Running, room.Status)
	room.TransitionTo(Fetching)
	assert.Equal(t, Fetching, room.Status)
}
