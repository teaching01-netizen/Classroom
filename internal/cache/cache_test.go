package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetStale_FreshData(t *testing.T) {
	c := New()
	c.Set("key1", "hello", 5*time.Minute)

	val, ok := c.GetStale("key1")
	if !ok {
		t.Fatal("expected ok=true for fresh key")
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %v", val)
	}
}

func TestGetStale_ExpiredData(t *testing.T) {
	c := New()
	// Use a zero TTL so the entry expires immediately
	c.Set("key2", "stale-data", 0)

	// Let the goroutine scheduler yield so time passes
	time.Sleep(time.Millisecond)

	// Get should return false
	_, ok := c.Get("key2")
	if ok {
		t.Fatal("expected Get to return false for expired key")
	}

	// GetStale should still return the data
	val, ok := c.GetStale("key2")
	if !ok {
		t.Fatal("expected GetStale to return true for expired key")
	}
	if val != "stale-data" {
		t.Fatalf("expected 'stale-data', got %v", val)
	}
}

func TestGetStale_MissingKey(t *testing.T) {
	c := New()

	val, ok := c.GetStale("nonexistent")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestGetStale_NegativeTTL(t *testing.T) {
	c := New()
	c.Set("neg", "data", -1*time.Minute)

	time.Sleep(time.Millisecond)

	val, ok := c.GetStale("neg")
	if !ok {
		t.Fatal("expected GetStale to return true for expired key (negative TTL)")
	}
	if val != "data" {
		t.Fatalf("expected 'data', got %v", val)
	}
}

func TestGetStale_AfterInvalidate(t *testing.T) {
	c := New()
	c.Set("tmp", "value", 5*time.Minute)
	c.Invalidate("tmp")

	val, ok := c.GetStale("tmp")
	if ok {
		t.Fatal("expected ok=false for invalidated key")
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestGetStale_ThreadSafe(t *testing.T) {
	c := New()
	c.Set("race", "safe", 5*time.Minute)

	done := make(chan struct{})
	go func() {
		c.GetStale("race")
		close(done)
	}()
	c.GetStale("race")
	<-done
}

func TestGet_FreshData(t *testing.T) {
	c := New()
	c.Set("k", "v", 5*time.Minute)

	val, ok := c.Get("k")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if val != "v" {
		t.Fatalf("expected 'v', got %v", val)
	}
}

func TestGet_ExpiredData(t *testing.T) {
	c := New()
	c.Set("k", "v", 0)
	time.Sleep(time.Millisecond)

	_, ok := c.Get("k")
	if ok {
		t.Fatal("expected ok=false for expired key")
	}
}

func TestGet_MissingKey(t *testing.T) {
	c := New()
	_, ok := c.Get("nope")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

// --- MarkStale tests ---

func TestMarkStale_ExtendsTTL(t *testing.T) {
	c := New()
	// Set with 1ms TTL so it expires fast.
	c.Set("k", "v", 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond)

	// Must be expired now.
	_, ok := c.Get("k")
	assert.False(t, ok, "must be expired before MarkStale")

	// MarkStale extends by 5 minutes.
	ok = c.MarkStale("k", 5*time.Minute)
	assert.True(t, ok, "MarkStale must return true for existing key")

	// Must be accessible via Get now (TTL extended).
	val, ok := c.Get("k")
	assert.True(t, ok, "Get must succeed after MarkStale extends TTL")
	assert.Equal(t, "v", val)
}

func TestMarkStale_StillStaleViaGetStale(t *testing.T) {
	c := New()
	c.Set("k", "v", 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond)

	// GetStale works even before MarkStale (entry exists, just expired).
	val, ok := c.GetStale("k")
	assert.True(t, ok)
	assert.Equal(t, "v", val)

	// After MarkStale, GetStale still works.
	c.MarkStale("k", 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	val, ok = c.GetStale("k")
	assert.True(t, ok, "GetStale must work after MarkStale even after secondary expiry")
	assert.Equal(t, "v", val)
}

func TestMarkStale_MissingKeyReturnsFalse(t *testing.T) {
	c := New()
	ok := c.MarkStale("nonexistent", 5*time.Minute)
	assert.False(t, ok, "MarkStale must return false for missing key")
}

func TestMarkStale_ConcurrentAccess(t *testing.T) {
	c := New()
	c.Set("k", "v", 5*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.MarkStale("k", 1*time.Second)
			c.Get("k")
			c.GetStale("k")
		}()
	}
	wg.Wait()
}

func TestMarkStale_DeltaCanBeNegative(t *testing.T) {
	c := New()
	c.Set("k", "v", 10*time.Minute)

	// Negative delta should shrink the TTL, potentially making it expire sooner.
	ok := c.MarkStale("k", -20*time.Minute)
	assert.True(t, ok)

	// Get should fail because the TTL is now in the past.
	time.Sleep(time.Millisecond)
	_, ok = c.Get("k")
	assert.False(t, ok, "negative delta should shrink TTL past expiry")
}
