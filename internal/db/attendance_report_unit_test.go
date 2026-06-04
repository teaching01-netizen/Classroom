package db

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttendanceReport_JSONKeysPinContract guards the on-the-wire JSON shape
// of the AttendanceReport payload. Phase 4 (persister) and Phase 5 (hydrator)
// serialize and deserialize this — if the keys change, persisted reports
// become unreadable on the next deploy. The contract is:
//
//   course_id, computed_at, threshold, duration_ms, payload
//
// plus payload being opaque JSONB (the domain.CourseAttendanceReport shape).
func TestAttendanceReport_JSONKeysPinContract(t *testing.T) {
	r := &AttendanceReport{
		CourseID:   "c-1",
		ComputedAt: time.Unix(0, 0).UTC(),
		Threshold:  4,
		DurationMs: 99,
		Payload:    []byte(`{"courseId":"c-1","students":[]}`),
	}

	// Direct struct marshal — used by code that wants a debug log line.
	topLevel, err := json.Marshal(r)
	require.NoError(t, err)
	s := string(topLevel)
	for _, key := range []string{
		`"course_id":"c-1"`,
		`"computed_at":`,
		`"threshold":4`,
		`"duration_ms":99`,
		`"payload":`,
	} {
		assert.True(t, strings.Contains(s, key),
			"AttendanceReport JSON must contain key %s, got %s", key, s)
	}
}

// TestAttendanceReport_PayloadIsRawJSON pins that Payload is stored and
// transported as raw JSON bytes — NOT re-marshaled as a nested struct.
// pgx marshals []byte as a JSON string; we want the inner object, so
// we use json.RawMessage-style bytes and let pgx treat it as jsonb directly.
func TestAttendanceReport_PayloadIsRawJSON(t *testing.T) {
	original := map[string]any{
		"courseId":   "c-1",
		"students":   []any{},
		"computedAt": "2026-01-01T00:00:00Z",
	}
	raw, err := json.Marshal(original)
	require.NoError(t, err)

	r := &AttendanceReport{CourseID: "c-1", Payload: raw}
	encoded, err := json.Marshal(r)
	require.NoError(t, err)

	// Decode the wrapper and verify payload is a nested object, not a string.
	var wrapper map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &wrapper))

	payload, ok := wrapper["payload"]
	require.True(t, ok, "encoded JSON must contain 'payload' key")
	assert.True(t, len(payload) > 0 && payload[0] == '{',
		"payload must serialize as a nested JSON object (got %s)", string(payload))
	// Crucially: the payload must NOT be a JSON string (which would be the
	// case if it were a plain []byte being marshaled).
	assert.NotEqual(t, byte('"'), payload[0],
		"payload must not be a JSON string (that would mean []byte was base64'd)")

	// And the inner object must contain the keys we put in.
	var inner map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &inner))
	assert.Equal(t, json.RawMessage(`"c-1"`), inner["courseId"])
	assert.Equal(t, json.RawMessage(`"2026-01-01T00:00:00Z"`), inner["computedAt"])
}
