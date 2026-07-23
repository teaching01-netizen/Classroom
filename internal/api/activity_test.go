package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/service"
)

type activityCounter struct {
	count atomic.Int64
}

func TestRouter_AdmittedRoomRequestsRecordActivityAfterRateLimit(t *testing.T) {
	counter := &activityCounter{}
	roomManager := service.NewRoomManager(nil, nil)
	router, rateLimiters := NewRouter(roomManager, nil, nil, nil, RouterOptions{
		WSMaxConns:       100,
		ActivityRecorder: counter,
	})
	defer rateLimiters.Stop()

	for index := 0; index < 21; index++ {
		req := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if index < 20 {
			require.NotEqual(t, http.StatusTooManyRequests, res.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, res.Code)
		}
	}
	require.Equal(t, int64(20), counter.count.Load(), "rejected requests must not extend activity")

	for _, path := range []string{"/api", "/metrics", "/api/teacherish"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
	require.Equal(t, int64(20), counter.count.Load(), "non-business routes must remain inactive")
}

func (c *activityCounter) RecordActivity() {
	c.count.Add(1)
}
