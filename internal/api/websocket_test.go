package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWSGuard_RejectsWhenAtLimit(t *testing.T) {
	wsConnCount.Store(3)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 3)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "too many WebSocket connections")
}

func TestWSGuard_AllowsUnderLimit(t *testing.T) {
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 10)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should NOT return 503 — will proceed to websocket.Accept which
	// fails with a non-503 error (no upgrade headers in test request).
	assert.NotEqual(t, http.StatusServiceUnavailable, rec.Code,
		"expected non-503 when under limit")
}

func TestWSGuard_CounterIncrementsAndDecrements(t *testing.T) {
	// Reset counter
	wsConnCount.Store(0)
	defer wsConnCount.Store(0)

	handler := wsHandler(nil, 10)

	// Capture counter just before the call (should be 0)
	before := wsConnCount.Load()
	require.Equal(t, int64(0), before, "counter should start at 0")

	// Make a request — handler will increment then attempt websocket.Accept,
	// which fails. The deferred decrement should still run.
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// After handler returns, counter should be back to 0
	after := wsConnCount.Load()
	assert.Equal(t, int64(0), after, "counter should be decremented after handler returns")
}
