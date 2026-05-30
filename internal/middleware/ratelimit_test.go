package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPRateLimiter_Allow_NewVisitorGetsFullBucket(t *testing.T) {
	rl := NewIPRateLimiter(10, 5)
	defer rl.Stop()

	// First request should always be allowed (full bucket).
	if !rl.Allow("192.168.1.1") {
		t.Error("expected first request to be allowed")
	}
}

func TestIPRateLimiter_Allow_ExhaustsBurst(t *testing.T) {
	rl := NewIPRateLimiter(100, 3) // high refill, burst 3
	defer rl.Stop()

	// First 3 requests within burst should pass.
	for i := 0; i < 3; i++ {
		if !rl.Allow("10.0.0.1") {
			t.Errorf("expected request %d to be allowed (burst)", i+1)
		}
	}
	// 4th request might be allowed if refill happened — use a very low rate
	// to guarantee exhaustion.
}

func TestIPRateLimiter_Allow_DeniedAfterBurstExceeded(t *testing.T) {
	rl := NewIPRateLimiter(0.001, 2) // very slow refill, burst 2
	defer rl.Stop()

	// Consume the burst.
	rl.Allow("10.0.0.2")
	rl.Allow("10.0.0.2")

	// Next should be denied (refill rate is negligible).
	if rl.Allow("10.0.0.2") {
		t.Error("expected request after burst to be denied")
	}
}

func TestIPRateLimiter_Allow_DifferentIPsAreIndependent(t *testing.T) {
	rl := NewIPRateLimiter(0.001, 1)
	defer rl.Stop()

	rl.Allow("10.0.0.1") // only token for IP A
	if !rl.Allow("10.0.0.2") {
		t.Error("expected different IP to have its own bucket")
	}
}

func TestIPRateLimiter_Allow_RefillOverTime(t *testing.T) {
	rl := NewIPRateLimiter(100, 1) // 100 tokens/sec, burst 1
	defer rl.Stop()

	// Consume the single token.
	rl.Allow("10.0.0.3")
	if rl.Allow("10.0.0.3") {
		t.Error("expected second immediate request to be denied")
	}

	// Wait enough for a refill (10ms should be enough at 100 tokens/sec).
	time.Sleep(15 * time.Millisecond)

	if !rl.Allow("10.0.0.3") {
		t.Error("expected request after refill to be allowed")
	}
}

func TestIPRateLimiter_Middleware_429Response(t *testing.T) {
	rl := NewIPRateLimiter(0.001, 0) // burst 0 — always deny
	defer rl.Stop()

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called when rate limited")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("expected Retry-After: 1, got %s", rec.Header().Get("Retry-After"))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("expected non-empty body")
	}
}

func TestIPRateLimiter_Middleware_PassesThroughAllowed(t *testing.T) {
	rl := NewIPRateLimiter(100, 5) // generous limit
	defer rl.Stop()

	var called bool
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected next handler to be called when rate limit not exceeded")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestExtractIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "198.51.100.1")
	req.RemoteAddr = "10.0.0.1:9999"

	ip := extractIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %s", ip)
	}
}

func TestExtractIP_XRealIPTakesPrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "198.51.100.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	req.RemoteAddr = "10.0.0.1:9999"

	ip := extractIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected X-Real-IP 198.51.100.1 to take precedence, got %s", ip)
	}
}

func TestExtractIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.1:8080"

	ip := extractIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %s", ip)
	}
}

func TestExtractIP_NoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.1"

	ip := extractIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %s", ip)
	}
}

func TestExtractIP_EmptyXFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "")
	req.RemoteAddr = "10.0.0.1:3000"

	ip := extractIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", ip)
	}
}

func TestIPRateLimiter_StaleCleanup(t *testing.T) {
	rl := NewIPRateLimiter(10, 5)
	rl.SetCleanupInterval(50 * time.Millisecond) // speed up for test
	defer rl.Stop()

	rl.Allow("stale-client")
	rl.mu.RLock()
	_, afterAllow := rl.visitors["stale-client"]
	rl.mu.RUnlock()
	if !afterAllow {
		t.Fatal("expected visitor to be created")
	}

	// Wait for cleanup.
	time.Sleep(100 * time.Millisecond)

	rl.mu.RLock()
	_, exists := rl.visitors["stale-client"]
	rl.mu.RUnlock()
	if exists {
		t.Error("expected stale visitor to be cleaned up")
	}
}

func TestIPRateLimiter_Stop_NoPanic(t *testing.T) {
	rl := NewIPRateLimiter(10, 5)
	rl.Stop() // should not panic
	rl.Stop() // double stop should not panic
}

func TestConcurrentAccess(t *testing.T) {
	rl := NewIPRateLimiter(0, 5) // no refill, burst 5 — exactly 5 tokens available
	defer rl.Stop()

	const goroutines = 20
	var allowed atomic.Int64
	var denied atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow("10.0.0.1") {
				allowed.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 5 {
		t.Errorf("expected exactly 5 allowed, got %d", got)
	}
	if got := denied.Load(); got != 15 {
		t.Errorf("expected exactly 15 denied, got %d", got)
	}
}
