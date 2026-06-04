package warwick

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewSharedTransport_ConfiguresAllTuningFields pins down the production
// transport config so a regression in any of these values (MaxIdleConns,
// IdleConnTimeout, TLSHandshakeTimeout, etc.) is caught immediately.
func TestNewSharedTransport_ConfiguresAllTuningFields(t *testing.T) {
	tr := NewSharedTransport(50)

	require.NotNil(t, tr)
	assert.Equal(t, 100, tr.MaxIdleConns, "MaxIdleConns must be at least 100 for shared use")
	assert.Equal(t, 50, tr.MaxIdleConnsPerHost, "MaxIdleConnsPerHost must match the connsPerHost arg")
	assert.Equal(t, 50, tr.MaxConnsPerHost, "MaxConnsPerHost must match the connsPerHost arg")
	assert.Equal(t, 90*time.Second, tr.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)
	assert.Equal(t, 1*time.Second, tr.ExpectContinueTimeout)
	// DisableCompression: false is what enables transparent gzip. We make it
	// explicit so it can't be flipped by accident.
	assert.False(t, tr.DisableCompression, "DisableCompression must be false so gzip responses decompress automatically")
	// Force HTTP/2 for connection multiplexing.
	assert.True(t, tr.ForceAttemptHTTP2, "ForceAttemptHTTP2 must be true")
}

// TestNewSharedTransport_HandlesGzipResponse asserts the end-to-end behavior:
// when a server returns Content-Encoding: gzip, the client transparently
// decompresses and the caller reads the plain body. This is a characterization
// test for the gzip support we depend on.
func TestNewSharedTransport_HandlesGzipResponse(t *testing.T) {
	const body = "warwick gzip test payload"
	var sawAcceptEncoding bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			sawAcceptEncoding = true
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(body))
		_ = gz.Close()
	}))
	defer srv.Close()

	tr := NewSharedTransport(10)
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(raw), "body must be transparently decompressed by the client")
	assert.True(t, sawAcceptEncoding, "client must send Accept-Encoding so server can compress")
}

// TestNewSharedTransport_PropagatesIntoPooledSessions pins the contract that
// the transport passed to NewSessionPool is the one every pooled session uses.
// This guarantees that production tuning (MaxIdleConns, gzip, HTTP/2) is
// actually applied to every request, not silently dropped.
func TestNewSharedTransport_PropagatesIntoPooledSessions(t *testing.T) {
	loginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=c; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer loginSrv.Close()

	tr := NewSharedTransport(20)
	pool, err := NewSessionPool("a@b", "pw", loginSrv.URL, 1, 1, 1, tr)
	require.NoError(t, err)

	// Acquire one session from each tier and verify all three use the same
	// transport we passed in.
	for _, tier := range []SessionTier{TierQR, TierTeacher, TierInteractive} {
		ref, err := pool.AcquireWithTimeout(tier, time.Second)
		require.NoError(t, err, "tier %d", tier)
		require.NotNil(t, ref.session, "tier %d", tier)
		assert.Same(t, tr, ref.session.client.Transport,
			"tier %d must use the shared transport, not a per-session DefaultTransport", tier)
		pool.Release(ref)
	}
}
