package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireCheckinLockSerializesSameStudent(t *testing.T) {
	repository, ctx := newSnapshotRepositoryTest(t)
	first, err := repository.AcquireCheckinLock(ctx, "session-1", "W123")
	require.NoError(t, err)

	type lockResult struct {
		lock CheckinLock
		err  error
	}
	secondResult := make(chan lockResult, 1)
	go func() {
		lock, acquireErr := repository.AcquireCheckinLock(ctx, "session-1", "W123")
		secondResult <- lockResult{lock: lock, err: acquireErr}
	}()

	select {
	case result := <-secondResult:
		if result.lock != nil {
			_ = result.lock.Release(context.Background())
		}
		t.Fatal("second mutation acquired the same student lock before release")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, first.Release(ctx))
	select {
	case result := <-secondResult:
		require.NoError(t, result.err)
		require.NoError(t, result.lock.Release(ctx))
	case <-time.After(2 * time.Second):
		t.Fatal("second mutation did not acquire the lock after release")
	}
}

func TestAcquireCheckinLockAllowsDifferentStudents(t *testing.T) {
	repository, ctx := newSnapshotRepositoryTest(t)
	first, err := repository.AcquireCheckinLock(ctx, "session-1", "W123")
	require.NoError(t, err)
	defer func() { _ = first.Release(context.Background()) }()

	secondCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	second, err := repository.AcquireCheckinLock(secondCtx, "session-1", "W456")
	require.NoError(t, err)
	require.NoError(t, second.Release(ctx))
}
