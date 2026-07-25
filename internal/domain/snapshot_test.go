package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotExpiryUsesLastValidation(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		ContentFetchedAt: now.Add(-30 * 24 * time.Hour),
		ValidatedAt:      now.Add(-time.Minute),
		MaxServeAge:      2 * time.Hour,
	}
	require.False(t, snapshot.Expired(now))
	require.True(t, snapshot.Expired(now.Add(3*time.Hour)))
}

func TestSnapshotExpiryFailsClosedForInvalidMaxServeAge(t *testing.T) {
	now := time.Now()
	require.True(t, (Snapshot{ValidatedAt: now, MaxServeAge: 0}).Expired(now))
	require.True(t, (Snapshot{ValidatedAt: now, MaxServeAge: -time.Second}).Expired(now))
}

func TestTargetRefIncludesParentIdentity(t *testing.T) {
	first := TargetRef{Host: "warwick", Kind: SnapshotSessionDetail, ParentKey: "course-1", ResourceKey: "session-1"}
	second := TargetRef{Host: "warwick", Kind: SnapshotSessionDetail, ParentKey: "course-2", ResourceKey: "session-1"}
	require.NotEqual(t, first, second)
	require.NotEqual(t, first.IdentityKey(), second.IdentityKey())
}

func TestContentHashZeroHelper(t *testing.T) {
	var hash [32]byte
	require.True(t, IsZeroContentHash(hash))
	hash[0] = 1
	require.False(t, IsZeroContentHash(hash))
}
