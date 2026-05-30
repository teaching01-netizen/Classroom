package cache

import (
	"testing"
	"time"
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
