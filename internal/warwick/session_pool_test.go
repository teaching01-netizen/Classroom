package warwick

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/metrics"
)

func TestAcquireWithTimeoutSucceedsImmediately(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	ref, err := pool.AcquireWithTimeout(TierQR, time.Second)
	require.NoError(t, err)
	require.NotNil(t, ref)
	assert.Equal(t, "testcookie", ref.Cookie)
	pool.Release(ref)
}

func TestAcquireWithTimeoutBlocksThenSucceeds(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	// Exhaust the QR tier
	ref1, err := pool.Acquire(TierQR)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	refCh := make(chan *SessionRef, 1)

	go func() {
		ref, acquireErr := pool.AcquireWithTimeout(TierQR, 5*time.Second)
		if acquireErr != nil {
			errCh <- acquireErr
			return
		}
		refCh <- ref
	}()

	// Give the goroutine time to block in cond.Wait()
	time.Sleep(100 * time.Millisecond)

	// Release the session — this should unblock the goroutine
	pool.Release(ref1)

	select {
	case ref2 := <-refCh:
		require.NotNil(t, ref2)
		assert.Equal(t, "testcookie", ref2.Cookie)
		pool.Release(ref2)
	case acquireErr := <-errCh:
		t.Fatalf("AcquireWithTimeout should have succeeded after release, got: %v", acquireErr)
	case <-time.After(2 * time.Second):
		t.Fatal("AcquireWithTimeout did not unblock within 2s of release")
	}
}

func TestAcquireWithTimeoutReturnsErrorOnTimeout(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	// Exhaust the QR tier
	ref1, err := pool.Acquire(TierQR)
	require.NoError(t, err)
	defer pool.Release(ref1)

	// Try to acquire with a very short timeout
	_, err = pool.AcquireWithTimeout(TierQR, 10*time.Millisecond)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNoAvailableSessions)
}

func TestSessionPoolReleaseIsIdempotent(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	ref, err := pool.Acquire(TierQR)
	require.NoError(t, err)
	pool.Release(ref)
	pool.Release(ref)

	ref, err = pool.Acquire(TierQR)
	require.NoError(t, err)
	defer pool.Release(ref)

	_, err = pool.AcquireWithTimeout(TierQR, 10*time.Millisecond)
	assert.ErrorIs(t, err, ErrNoAvailableSessions)
}

func TestAcquireTimeoutIncrementsExhaustionMetric(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	before := testutil.ToFloat64(metrics.WarwickSessionPoolExhaustedTotal.WithLabelValues(TierQR.String()))

	ref, err := pool.Acquire(TierQR)
	require.NoError(t, err)
	defer pool.Release(ref)

	_, err = pool.AcquireWithTimeout(TierQR, 10*time.Millisecond)
	require.ErrorIs(t, err, ErrNoAvailableSessions)

	after := testutil.ToFloat64(metrics.WarwickSessionPoolExhaustedTotal.WithLabelValues(TierQR.String()))
	assert.Equal(t, before+1, after, "a timed-out acquisition should bump the exhaustion counter")
}

func TestAcquireWithTimeoutContextHonorsCancellation(t *testing.T) {
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	ref, err := pool.Acquire(TierQR)
	require.NoError(t, err)
	defer pool.Release(ref)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pool.AcquireWithTimeoutContext(ctx, TierQR, time.Second)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestAcquireWithTimeoutContextCancelsLogin(t *testing.T) {
	loginStarted := make(chan struct{})
	var requests atomic.Int32
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(loginStarted)
			time.Sleep(100 * time.Millisecond)
			return
		}
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, acquireErr := pool.AcquireWithTimeoutContext(ctx, TierQR, time.Second)
		result <- acquireErr
	}()
	<-loginStarted
	cancel()
	assert.ErrorIs(t, <-result, context.Canceled)

	ref, err := pool.AcquireWithTimeout(TierQR, time.Second)
	require.NoError(t, err)
	pool.Release(ref)
}

func TestCanceledRefreshDoesNotBackoffHealthySession(t *testing.T) {
	loginStarted := make(chan struct{})
	var requests atomic.Int32
	loginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			w.Header().Add("Set-Cookie", "ASP.NET_SessionId=initial; path=/; HttpOnly")
			w.WriteHeader(http.StatusFound)
		case 2:
			close(loginStarted)
			time.Sleep(100 * time.Millisecond)
		default:
			w.Header().Add("Set-Cookie", "ASP.NET_SessionId=refreshed; path=/; HttpOnly")
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer loginServer.Close()

	pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
	require.NoError(t, err)

	ref, err := pool.Acquire(TierQR)
	require.NoError(t, err)
	pool.Release(ref)

	session := pool.sessions[0]
	session.mu.Lock()
	session.expiresAt = time.Now().Add(-time.Minute)
	session.obtainedAt = time.Now()
	session.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, acquireErr := pool.AcquireWithTimeoutContext(ctx, TierQR, time.Second)
		result <- acquireErr
	}()
	<-loginStarted
	cancel()
	assert.ErrorIs(t, <-result, context.Canceled)

	ref, err = pool.AcquireWithTimeout(TierQR, time.Second)
	require.NoError(t, err, "a canceled refresh must not back off the session")
	pool.Release(ref)
}
