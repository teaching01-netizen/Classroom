package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiterEntry holds the token bucket state for a single IP.
type rateLimiterEntry struct {
	tokens     float64
	lastRefill time.Time
}

// IPRateLimiter implements a per-IP token-bucket rate limiter.
type IPRateLimiter struct {
	mu              sync.RWMutex
	visitors        map[string]*rateLimiterEntry
	rate            float64       // tokens added per second
	burst           int           // max allowed tokens (bucket capacity)
	cleanupInterval time.Duration // how often to reap stale entries
	stopCh          chan struct{}
	stopOnce        sync.Once
}

// NewIPRateLimiter creates a new per-IP rate limiter.
// rate: number of tokens added per second.
// burst: maximum bucket capacity (and thus max burst size).
func NewIPRateLimiter(rate float64, burst int) *IPRateLimiter {
	l := &IPRateLimiter{
		visitors:        make(map[string]*rateLimiterEntry),
		rate:            rate,
		burst:           burst,
		cleanupInterval: 5 * time.Minute,
		stopCh:          make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Stop terminates the background cleanup goroutine. Safe to call multiple times.
func (l *IPRateLimiter) Stop() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
}

// Allow checks whether the given IP is allowed to proceed.
// It refills the token bucket on each call, then consumes one token if available.
func (l *IPRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.visitors[ip]
	if !ok {
		// New visitor — start with a full bucket.
		entry = &rateLimiterEntry{
			tokens:     float64(l.burst),
			lastRefill: time.Now(),
		}
		l.visitors[ip] = entry
	}

	// Refill tokens based on elapsed time.
	now := time.Now()
	elapsed := now.Sub(entry.lastRefill).Seconds()
	entry.tokens += elapsed * l.rate
	if entry.tokens > float64(l.burst) {
		entry.tokens = float64(l.burst)
	}
	entry.lastRefill = now

	if entry.tokens >= 1.0 {
		entry.tokens--
		return true
	}
	return false
}

// Middleware returns an http.Handler that rate-limits per client IP.
func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		if !l.Allow(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":         "rate limit exceeded",
				"retry_after_ms": 1000,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractIP gets the real client IP.
//
// Security assumption: this service runs either directly exposed (RemoteAddr is the
// source of truth) or behind a reverse proxy that sets X-Real-IP.
// X-Forwarded-For is NOT trusted because it can be trivially spoofed by clients.
func extractIP(r *http.Request) string {
	// X-Real-IP is set by reverse proxies (nginx, caddy, etc.) and is more trustworthy.
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// SetCleanupInterval sets the interval at which stale entries are removed.
// It is safe to call before any Allow/Middleware calls, but should not be
// called concurrently with cleanup runs (use during initial setup only).
func (l *IPRateLimiter) SetCleanupInterval(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupInterval = d
}

// cleanupLoop periodically removes entries that haven't been seen for the cleanup interval.
func (l *IPRateLimiter) cleanupLoop() {
	l.mu.Lock()
	interval := l.cleanupInterval
	l.mu.Unlock()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.removeStale()
		case <-l.stopCh:
			return
		}
	}
}

// removeStale deletes entries whose lastRefill is older than the cleanup interval.
func (l *IPRateLimiter) removeStale() {
	l.mu.Lock()
	defer l.mu.Unlock()
	threshold := time.Now().Add(-l.cleanupInterval)
	for ip, entry := range l.visitors {
		if entry.lastRefill.Before(threshold) {
			delete(l.visitors, ip)
		}
	}
}
