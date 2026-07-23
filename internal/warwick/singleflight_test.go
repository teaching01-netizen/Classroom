package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// ============================================================================
// Singleflight coalescing tests
// ============================================================================

// TestSingleflight_GetCourses_ConcurrentIdenticalSharesOneUpstream verifies
// that two concurrent identical calls to GetCourses share one upstream request
// (VAL-CONCUR-001).
func TestSingleflight_GetCourses_ConcurrentIdenticalSharesOneUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		// Slow down so concurrent calls overlap.
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan *domain.CourseDetail, 2)
	errs := make(chan error, 2)

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detail, err := client.GetCourseDetailWithName(ctx, "c1", "Math 101")
			if err != nil {
				errs <- err
				return
			}
			results <- detail
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	assert.Equal(t, int32(1), upstreamCalls.Load(), "expected 1 upstream call for 2 concurrent requests")

	var details []*domain.CourseDetail
	for d := range results {
		details = append(details, d)
	}
	require.Len(t, details, 2)
	require.NotNil(t, details[0])
	require.NotNil(t, details[1])
	assert.Equal(t, len(details[0].Sessions), len(details[1].Sessions))
}

// TestSingleflight_GetCourses_SequentialCallsProducesTwoUpstream verifies
// that sequential calls produce two upstream requests (no caching) (VAL-CONCUR-003, 004).
func TestSingleflight_GetCourses_SequentialCallsProducesTwoUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	ctx := context.Background()

	// First call
	_, err = client.GetCourseDetailWithName(ctx, "c1", "Math 101")
	require.NoError(t, err)

	// Second call (sequential, same params)
	_, err = client.GetCourseDetailWithName(ctx, "c1", "Math 101")
	require.NoError(t, err)

	assert.Equal(t, int32(2), upstreamCalls.Load(), "expected 2 upstream calls for 2 sequential requests")
}

// TestSingleflight_GetCourses_DifferentKeysSeparateCalls verifies that
// different keys produce separate upstream calls (VAL-CONCUR-002).
func TestSingleflight_GetCourses_DifferentKeysSeparateCalls(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		time.Sleep(30 * time.Millisecond) // ensure overlap
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	ctx := context.Background()
	var wg sync.WaitGroup

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.GetCourseDetailWithName(ctx, "c1", "Math 101")
		}()
	}

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.GetCourseDetailWithName(ctx, "c2", "Physics 201")
		}()
	}
	wg.Wait()

	calls := upstreamCalls.Load()
	assert.GreaterOrEqual(t, calls, int32(2), "expected at least 2 upstream calls for 2 distinct keys")
}

// TestSingleflight_FetchSessionDetailLive_ConcurrentIdentical verifies
// that two concurrent identical calls share one upstream call.
func TestSingleflight_FetchSessionDetailLive_ConcurrentIdentical(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		time.Sleep(50 * time.Millisecond) // ensure overlap
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentName":"Alice","StudentNickName":"","StudentSchool":"Science","StudentCheckIn":true,"StudentPPoint":0}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan *domain.SessionDetail, 2)
	errs := make(chan error, 2)

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detail, err := client.FetchSessionDetailLive(ctx, "s1")
			if err != nil {
				errs <- err
				return
			}
			results <- detail
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	assert.Equal(t, int32(1), upstreamCalls.Load(), "expected 1 upstream call for 2 concurrent identical FetchSessionDetailLive requests")

	var details []*domain.SessionDetail
	for d := range results {
		details = append(details, d)
	}
	require.Len(t, details, 2)
	require.NotNil(t, details[0])
	require.NotNil(t, details[1])
}

// TestSingleflight_FetchStudentProfiles_ConcurrentIdentical verifies
// that two concurrent identical calls share one upstream call.
func TestSingleflight_FetchStudentProfiles_ConcurrentIdentical(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentGuid":"guid-1","FullName":"Alice","School":"Science"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL

	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan []domain.StudentProfile, 2)
	errs := make(chan error, 2)

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			profiles, err := client.FetchStudentProfiles(ctx)
			if err != nil {
				errs <- err
				return
			}
			results <- profiles
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	assert.Equal(t, int32(1), upstreamCalls.Load(), "expected 1 upstream call for 2 concurrent identical FetchStudentProfiles requests")

	var profiles [][]domain.StudentProfile
	for p := range results {
		profiles = append(profiles, p)
	}
	require.Len(t, profiles, 2)
	require.Len(t, profiles[0], 1)
	require.Len(t, profiles[1], 1)
	assert.Equal(t, profiles[0][0].FullName, profiles[1][0].FullName)
}

// TestSingleflight_CancelledWaiterReturnsOwnError verifies that a waiter
// with a cancelled context returns its own context error without cancelling
// the shared call for other waiters (VAL-CONCUR-005).
func TestSingleflight_CancelledWaiterReturnsOwnError(t *testing.T) {
	proceedBlock := make(chan struct{})

	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		<-proceedBlock // Block until the test allows
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	// Goroutine A: starts the shared call, blocks on the server.
	ctxA := context.Background()
	type resultA struct {
		detail *domain.CourseDetail
		err    error
	}
	chA := make(chan resultA, 1)
	go func() {
		detail, err := client.GetCourseDetailWithName(ctxA, "c1", "Math 101")
		chA <- resultA{detail, err}
	}()

	// Give goroutine A time to start and enter the singleflight.
	time.Sleep(20 * time.Millisecond)

	// Goroutine B: joins the same singleflight, then cancels.
	ctxB, cancelB := context.WithCancel(context.Background())
	errBch := make(chan error, 1)
	go func() {
		_, err := client.GetCourseDetailWithName(ctxB, "c1", "Math 101")
		errBch <- err
	}()

	// Give goroutine B time to join the singleflight.
	time.Sleep(20 * time.Millisecond)

	// Cancel goroutine B's context.
	cancelB()

	// B should return quickly with context.Canceled.
	select {
	case err := <-errBch:
		assert.ErrorIs(t, err, context.Canceled, "cancelled waiter should return context.Canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled waiter")
	}

	// Now unblock the server so A can complete.
	close(proceedBlock)

	// A should complete successfully.
	select {
	case r := <-chA:
		require.NoError(t, r.err)
		require.NotNil(t, r.detail)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for non-cancelled caller")
	}

	// Exactly 1 upstream call should have been made despite two callers.
	assert.Equal(t, int32(1), upstreamCalls.Load(), "expected 1 upstream call despite cancelled waiter")
}

// TestSingleflight_CancelledWaiterNoSessionLeak verifies a cancelled waiter
// does not leak a pooled session (VAL-CONCUR-006).
func TestSingleflight_CancelledWaiterNoSessionLeak(t *testing.T) {
	proceedBlock := make(chan struct{})

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-proceedBlock
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)
	// Use a pool with only 1 teacher session.
	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	client := NewClassroomClientFromPool(pool, TierTeacher)
	client.baseURL = apiServer.URL
	client.SetUserID("test-user")

	// Goroutine A starts the shared call (acquires the pool session, blocks on HTTP).
	ctxA := context.Background()
	chA := make(chan error, 1)
	go func() {
		_, err := client.GetCourseDetailWithName(ctxA, "c1", "Math 101")
		chA <- err
	}()

	// Give A time to start and acquire the pool session.
	time.Sleep(50 * time.Millisecond)

	// Goroutine B joins and cancels.
	ctxB, cancelB := context.WithCancel(context.Background())
	errBch := make(chan error, 1)
	go func() {
		_, err := client.GetCourseDetailWithName(ctxB, "c1", "Math 101")
		errBch <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancelB()

	// B should return quickly with context.Canceled.
	select {
	case err := <-errBch:
		assert.ErrorIs(t, err, context.Canceled, "cancelled waiter should return context.Canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled waiter")
	}

	// Let A complete.
	close(proceedBlock)
	select {
	case err := <-chA:
		require.NoError(t, err, "non-cancelled caller should succeed")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for non-cancelled caller")
	}

	// After A completes, the pool session should be released.
	// Try to acquire a session - should succeed immediately.
	ref, err := pool.AcquireWithTimeoutContext(context.Background(), TierTeacher, 1*time.Second)
	require.NoError(t, err, "should be able to acquire a session after cancelled waiter returns")
	pool.Release(ref)
}

func TestSingleflight_DeterministicRepeatedRuns(t *testing.T) {
	for i := 0; i < 3; i++ {
		var upstreamCalls atomic.Int32
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalls.Add(1)
			time.Sleep(30 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"StudentID":"STU001","StudentName":"Alice","StudentNickName":"","StudentSchool":"Science","StudentCheckIn":true,"StudentPPoint":0}]}`))
		}))
		t.Cleanup(apiServer.Close)

		loginServer := newTestLoginServer(t)
		pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
		require.NoError(t, err)
		client := NewClassroomClientFromPool(pool, TierTeacher)
		client.baseURL = apiServer.URL

		ctx := context.Background()
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = client.FetchSessionDetailLive(ctx, "s1")
			}()
		}
		wg.Wait()

		assert.Equal(t, int32(1), upstreamCalls.Load(),
			"iteration %d: expected 1 upstream call for 2 concurrent identical requests", i)
	}
}

// TestSingleflight_GetCourses_DifferentUserIDsSeparateCalls verifies that
// different UserIDs produce different singleflight keys.
func TestSingleflight_GetCourses_DifferentUserIDsSeparateCalls(t *testing.T) {
	var upstreamCalls atomic.Int32

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "ClassAttendanceDetailSearch") {
			_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":2,"recordsFiltered":2,"data":[{"dID":"s1","dName":"Week 1","dStatus":"Finished"},{"dID":"s2","dName":"Week 2","dStatus":"Finished"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"draw":1,"recordsTotal":1,"recordsFiltered":1,"data":[{"ID":"c1","CourseName":"Math 101","Cycle":"","Enrolled":30,"StartDate":"2020-01-01T00:00:00","EndDate":"2099-12-31T23:59:59"}]}`))
	}))
	t.Cleanup(apiServer.Close)

	loginServer := newTestLoginServer(t)

	pool1, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client1 := NewClassroomClientFromPool(pool1, TierTeacher)
	client1.baseURL = apiServer.URL
	client1.SetUserID("user-1")

	pool2, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 3, 5, 1)
	require.NoError(t, err)
	client2 := NewClassroomClientFromPool(pool2, TierTeacher)
	client2.baseURL = apiServer.URL
	client2.SetUserID("user-2")

	ctx := context.Background()
	var wg sync.WaitGroup

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client1.GetCourses(ctx)
		}()
	}

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client2.GetCourses(ctx)
		}()
	}
	wg.Wait()

	calls := upstreamCalls.Load()
	assert.GreaterOrEqual(t, calls, int32(2), "expected at least 2 upstream calls for 2 different UserIDs")
}
