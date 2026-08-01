package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{AuthExpired, Fetching}, {AuthExpired, Stopped},
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
		{AuthExpired, Warning},
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
	assert.Equal(t, Warning, ErrRateLimited.ToRoomStatus())
	assert.Equal(t, Warning, ErrAuthConflict.ToRoomStatus())
	assert.Equal(t, Warning, ErrPoolExhausted.ToRoomStatus())
}

func TestErrPoolExhausted(t *testing.T) {
	assert.Equal(t, "pool exhausted — all sessions in use", ErrPoolExhausted.Error())
	assert.Equal(t, ErrKindPoolExhausted, ErrPoolExhausted.Kind)
	assert.IsType(t, &FetchError{}, ErrPoolExhausted)
}

func TestErrPoolExhaustedToRoomStatus(t *testing.T) {
	assert.Equal(t, Warning, ErrPoolExhausted.ToRoomStatus())
}

func TestRoomTransitionTo(t *testing.T) {
	room := NewRoom("r1", "c1", nil)
	assert.Equal(t, Idle, room.Status)
	assert.NoError(t, room.TransitionTo(Running))
	assert.Equal(t, Running, room.Status)
	assert.NoError(t, room.TransitionTo(Fetching))
	assert.Equal(t, Fetching, room.Status)
}

func TestRoomTransitionToInvalid(t *testing.T) {
	room := NewRoom("r2", "c1", nil)
	assert.Equal(t, Idle, room.Status)
	err := room.TransitionTo(Fetching)
	assert.Error(t, err)
	assert.Equal(t, Idle, room.Status)
}

func TestCalculateNextFetchDelayZero(t *testing.T) {
	assert.Equal(t, uint64(0), CalculateNextFetchDelay(0))
}

func TestRoomLiteJSONOmitsQRURL(t *testing.T) {
	qr := strings.Repeat("A", 64*1024)
	name := "room"
	room := Room{
		RoomID:  "r1",
		ClassID: "c1",
		Name:    &name,
		Status:  Running,
		QRURL:   &qr,
	}
	data, err := json.Marshal(NewRoomLite(room))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "qr_url")
	assert.NotContains(t, string(data), qr)
}

func TestRoomsToLiteDoesNotCopyQRValues(t *testing.T) {
	qr := strings.Repeat("B", 64*1024)
	name := "room"
	room := Room{
		RoomID:  "r1",
		ClassID: "c1",
		Name:    &name,
		Status:  Running,
		QRURL:   &qr,
	}
	lite := RoomsToLite([]Room{room})
	require.Len(t, lite, 1)
	assert.Equal(t, "r1", lite[0].RoomID)
	assert.Equal(t, Running, lite[0].Status)
	require.NotNil(t, room.QRURL, "source room keeps its QR payload")

	data, err := json.Marshal(lite)
	require.NoError(t, err)
	assert.NotContains(t, string(data), qr)
}

func TestRoomLiteSummarySmallWithLargeQR(t *testing.T) {
	qr := strings.Repeat("C", 64*1024)
	room := Room{
		RoomID:  "r1",
		ClassID: "c1",
		Status:  Running,
		QRURL:   &qr,
	}
	data, err := json.Marshal(RoomsToLite([]Room{room}))
	require.NoError(t, err)
	assert.Less(t, len(data), 512)
}
