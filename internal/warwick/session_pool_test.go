package warwick

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
