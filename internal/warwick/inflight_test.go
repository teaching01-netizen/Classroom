package warwick

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInflightGroupCoalescesConcurrentCallers(t *testing.T) {
	var group inflightGroup
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	producer := func(ctx context.Context) (any, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return "value", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	firstDone := make(chan struct{})
	go func() {
		value, err, shared := group.Do(context.Background(), "key", producer)
		require.Equal(t, "value", value)
		require.NoError(t, err)
		require.False(t, shared)
		close(firstDone)
	}()

	<-started
	secondDone := make(chan struct{})
	go func() {
		value, err, shared := group.Do(context.Background(), "key", producer)
		require.Equal(t, "value", value)
		require.NoError(t, err)
		require.True(t, shared)
		close(secondDone)
	}()

	require.Eventually(t, func() bool {
		group.mu.Lock()
		defer group.mu.Unlock()
		call := group.calls["key"]
		return call != nil && call.waiters == 2
	}, time.Second, time.Millisecond)

	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first caller did not complete")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second caller did not complete")
	}
	require.Equal(t, int32(1), calls.Load())
}

func TestInflightGroupCancelsProducerWhenLastWaiterLeaves(t *testing.T) {
	var group inflightGroup
	producerStarted := make(chan struct{})
	producerCanceled := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, err, _ := group.Do(ctx, "key", func(ctx context.Context) (any, error) {
			close(producerStarted)
			<-ctx.Done()
			close(producerCanceled)
			return nil, ctx.Err()
		})
		require.ErrorIs(t, err, context.Canceled)
		close(done)
	}()

	<-producerStarted
	cancel()
	select {
	case <-producerCanceled:
	case <-time.After(time.Second):
		t.Fatal("producer was not cancelled after the last waiter left")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("caller did not return after cancellation")
	}
}

func TestInflightGroupCancelsOnlyTheLeavingWaiter(t *testing.T) {
	var group inflightGroup
	producerStarted := make(chan struct{})
	release := make(chan struct{})
	producerCanceled := make(chan struct{})

	firstDone := make(chan error, 1)
	go func() {
		_, err, _ := group.Do(context.Background(), "key", func(ctx context.Context) (any, error) {
			close(producerStarted)
			select {
			case <-release:
				return "ok", nil
			case <-ctx.Done():
				close(producerCanceled)
				return nil, ctx.Err()
			}
		})
		firstDone <- err
	}()

	<-producerStarted
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err, _ := group.Do(ctx, "key", func(context.Context) (any, error) {
			return nil, nil
		})
		secondDone <- err
	}()
	cancel()
	require.ErrorIs(t, <-secondDone, context.Canceled)

	close(release)
	require.NoError(t, <-firstDone)
	select {
	case <-producerCanceled:
		t.Fatal("producer was cancelled while another waiter remained")
	default:
	}
}

func TestInflightGroupConvertsProducerPanicToError(t *testing.T) {
	var group inflightGroup
	done := make(chan error, 1)
	go func() {
		_, err, _ := group.Do(context.Background(), "panic-key", func(context.Context) (any, error) {
			panic("boom")
		})
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		require.ErrorContains(t, err, "in-flight producer panic")
	case <-time.After(time.Second):
		t.Fatal("panic producer left waiter blocked")
	}
}

func TestCourseDetailSingleflightKeyDoesNotCollideOnDelimiters(t *testing.T) {
	first := courseDetailSingleflightKey("course:a", "name")
	second := courseDetailSingleflightKey("course", "a:name")
	require.NotEqual(t, first, second)
}
