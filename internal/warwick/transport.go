package warwick

import (
	"net/http"
	"time"
)

// NewSharedTransport returns the *http.Transport used for all outbound
// calls to Warwick. Centralising the config here lets us pin the tuning
// values in tests and reuse the same transport across all pooled sessions
// (so connection multiplexing, keep-alive, and HTTP/2 actually work).
//
// Tuning rationale:
//   - MaxIdleConns=100: enough headroom for the 6-session pool with bursts.
//   - MaxIdleConnsPerHost=connsPerHost: the upstream is one host; idle conns
//     per host should be >= max concurrent in-flight per host.
//   - MaxConnsPerHost=connsPerHost: hard cap on in-flight per host. The
//     semaphore in ComputeCourseAttendanceReport bounds per-report fan-out
//     (2), but the global per-host cap protects us from aggregate bursts.
//   - IdleConnTimeout=90s: long enough to survive slow report refreshes.
//   - TLSHandshakeTimeout=10s: matches the http.Client.Timeout in pool.
//   - DisableCompression=false (explicit): Go's HTTP client transparently
//     requests gzip via Accept-Encoding and decompresses the response. We
//     pin this to false so it can't be flipped by accident.
//   - ForceAttemptHTTP2=true: enables HTTP/2 multiplexing when the upstream
//     supports it.
func NewSharedTransport(connsPerHost int) *http.Transport {
	return &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   connsPerHost,
		MaxConnsPerHost:       connsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
		ForceAttemptHTTP2:     true,
	}
}
