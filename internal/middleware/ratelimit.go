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

// extractIP attempts to get the real client IP from headers, falling back to RemoteAddr.
func extractIP(r *http.Request) string {
	// Prefer X-Forwarded-For.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// cleanupLoop periodically removes entries that haven't been seen for the cleanup interval.
func (l *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupInterval)
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
