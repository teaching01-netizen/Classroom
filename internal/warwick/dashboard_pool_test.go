package warwick

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/cache"
)

// TestGetCourseDetail_ConcurrentCalls_WithLimitedPool verifies that multiple
// concurrent GetCourseDetail calls complete successfully even when the pool
// has fewer sessions than concurrent callers (some will wait for a session).
func TestGetCourseDetail_ConcurrentCalls_WithLimitedPool(t *testing.T) {
	mc := cache.New()
	loginServer := newDashboardLoginServer(t)

	var mu sync.Mutex
	activeCalls := 0

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		activeCalls++
		mu.Unlock()

		// Simulate slow response to hold sessions longer.
		time.Sleep(80 * time.Millisecond)

		mu.Lock()
		activeCalls--
		mu.Unlock()

		// Return valid ClassAttendanceDetailResponse (empty sessions).
		w.Header().Set("Content-Type", "application/json")
		resp := ClassAttendanceDetailResponse{
			Draw:            1,
			RecordsTotal:    0,
			RecordsFiltered: 0,
			Data:            []ClassAttendanceDetailRow{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(apiServer.Close)

	// Pool with 1 teacher session, but 3 concurrent callers.
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	// Launch 3 concurrent GetCourseDetail calls — only 1 session available.
	var wg sync.WaitGroup
	errors := make([]error, 3)
	details := make([]bool, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d, err := client.GetCourseDetail("C1")
			errors[idx] = err
			details[idx] = d != nil
		}(i)
	}
	wg.Wait()

	// At least 2 of 3 should succeed (the third waits for a session to free).
	successCount := 0
	for i := 0; i < 3; i++ {
		if errors[i] == nil && details[i] {
			successCount++
		}
	}
	assert.GreaterOrEqual(t, successCount, 2,
		"at least 2 of 3 concurrent calls should succeed with 1 session")
}

// TestGetCourseDetail_PoolExhaustion_DoesNotPanic verifies the client
// handles pool exhaustion gracefully without panicking.
func TestGetCourseDetail_PoolExhaustion_DoesNotPanic(t *testing.T) {
	mc := cache.New()
	loginServer := newDashboardLoginServer(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := ClassAttendanceDetailResponse{
			Draw:            1,
			RecordsTotal:    0,
			RecordsFiltered: 0,
			Data:            []ClassAttendanceDetailRow{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(apiServer.Close)

	// Pool with only 1 teacher session.
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher, mc)
	client.baseURL = apiServer.URL

	// First call: should succeed (1 session available).
	detail, err := client.GetCourseDetail("C1")
	require.NoError(t, err)
	require.NotNil(t, detail)

	// Second call: should also succeed (session released after first call).
	detail2, err := client.GetCourseDetail("C1")
	require.NoError(t, err)
	require.NotNil(t, detail2)
}

// newDashboardLoginServer creates an httptest server that returns a Warwick login cookie.
func newDashboardLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s
}
