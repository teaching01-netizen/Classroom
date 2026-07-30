package scraper

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestV1CanonicalizerImplementsInterface(t *testing.T) {
	var _ Canonicalizer = &V1Canonicalizer{}
}

func TestV1CanonicalizerVersion(t *testing.T) {
	c := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}
	require.Equal(t, 1, c.Version())
}

func TestCanonicalizationIndependentOfInputOrdering(t *testing.T) {
	c := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}

	courses := []domain.CourseSummary{
		{CourseID: "c3", Name: "Charlie"},
		{CourseID: "c1", Name: "Alpha"},
		{CourseID: "c2", Name: "Bravo"},
	}
	reordered := []domain.CourseSummary{courses[1], courses[2], courses[0]}

	_, hash1, count1, err := c.Canonicalize(domain.SnapshotCourseCatalog, courses)
	require.NoError(t, err)
	_, hash2, count2, err := c.Canonicalize(domain.SnapshotCourseCatalog, reordered)
	require.NoError(t, err)
	require.Equal(t, hash1, hash2, "hash must be independent of input order")
	require.Equal(t, count1, count2, "record count must match")
}

func TestCanonicalizationStableAcrossProcesses(t *testing.T) {
	c1 := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}
	c2 := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}

	profiles := []domain.StudentProfile{
		{StudentGuid: "g2", StudentID: "s2", FullName: "Bob"},
		{StudentGuid: "g1", StudentID: "s1", FullName: "Alice"},
	}

	payload1, hash1, _, err := c1.Canonicalize(domain.SnapshotStudentProfiles, profiles)
	require.NoError(t, err)
	payload2, hash2, _, err := c2.Canonicalize(domain.SnapshotStudentProfiles, profiles)
	require.NoError(t, err)
	require.Equal(t, payload1, payload2, "payload must be deterministic")
	require.Equal(t, hash1, hash2, "hash must be deterministic")
}

func TestCanonicalizationUsesCompleteTieBreaker(t *testing.T) {
	c := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}

	// Two profiles with same StudentGuid and StudentID but different FullName.
	// The tertiary tie-breaker (FullName) must produce a deterministic order.
	profiles := []domain.StudentProfile{
		{StudentGuid: "g1", StudentID: "s1", FullName: "Zoe"},
		{StudentGuid: "g1", StudentID: "s1", FullName: "Alice"},
	}
	_, hash, _, err := c.Canonicalize(domain.SnapshotStudentProfiles, profiles)
	require.NoError(t, err)

	reversed := []domain.StudentProfile{profiles[1], profiles[0]}
	_, hashRev, _, err := c.Canonicalize(domain.SnapshotStudentProfiles, reversed)
	require.NoError(t, err)
	require.Equal(t, hash, hashRev, "hash must be stable regardless of input order when tie-breakers apply")
}

func TestVolatileFieldsDoNotChangeHash(t *testing.T) {
	c := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}

	qrExpiry1 := "2026-07-30T10:00:00Z"
	qrExpiry2 := "2026-07-30T17:00:00+07:00" // same instant, different representation

	checkinAt1 := "2026-07-30T10:00:00Z"
	checkinAt2 := "2026-07-30T17:00:00+07:00"

	s1 := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "sess1"},
		Students: []domain.StudentCheckin{
			{StudentID: "st1", Name: "Alice", CheckedInAt: &checkinAt1},
		},
		QRExpiresAt: &qrExpiry1,
	}
	s2 := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "sess1"},
		Students: []domain.StudentCheckin{
			{StudentID: "st1", Name: "Alice", CheckedInAt: &checkinAt2},
		},
		QRExpiresAt: &qrExpiry2,
	}

	_, hash1, _, err := c.Canonicalize(domain.SnapshotSessionDetail, s1)
	require.NoError(t, err)
	_, hash2, _, err := c.Canonicalize(domain.SnapshotSessionDetail, s2)
	require.NoError(t, err)
	require.Equal(t, hash1, hash2, "timestamp offset representations must normalize to the same hash")
}

func TestV1CanonicalizerMatchesLegacyFunction(t *testing.T) {
	c := &V1Canonicalizer{MaxPayloadBytes: 1 << 20}

	courses := []domain.CourseSummary{
		{CourseID: "a", Name: "Alpha"},
		{CourseID: "b", Name: "Bravo"},
	}

	v1Payload, v1Hash, v1Count, err := c.Canonicalize(domain.SnapshotCourseCatalog, courses)
	require.NoError(t, err)

	legacyPayload, legacyHash, legacyCount, err := Canonicalize(domain.SnapshotCourseCatalog, courses, 1<<20)
	require.NoError(t, err)
	require.Equal(t, v1Payload, json.RawMessage(legacyPayload))
	require.Equal(t, v1Hash, legacyHash)
	require.Equal(t, v1Count, legacyCount)
}
