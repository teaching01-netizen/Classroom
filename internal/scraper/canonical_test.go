package scraper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestCanonicalizeStableOrderingWithoutMutatingCaller(t *testing.T) {
	catalog := []domain.CourseSummary{
		{CourseID: "b", Name: "Beta"},
		{CourseID: "a", Name: "Zulu"},
		{CourseID: "a", Name: "Alpha"},
	}
	original := append([]domain.CourseSummary(nil), catalog...)
	firstJSON, firstHash, err := Canonicalize(domain.SnapshotCourseCatalog, catalog, 1<<20)
	require.NoError(t, err)
	require.Equal(t, original, catalog)

	reordered := []domain.CourseSummary{catalog[2], catalog[0], catalog[1]}
	secondJSON, secondHash, err := Canonicalize(domain.SnapshotCourseCatalog, reordered, 1<<20)
	require.NoError(t, err)
	require.Equal(t, firstJSON, secondJSON)
	require.Equal(t, firstHash, secondHash)
}

func TestCanonicalizeNestedListsAndNilNormalization(t *testing.T) {
	courseA := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "course"},
		Sessions: []domain.SessionSummary{
			{SessionID: "b", SessionNumber: 2},
			{SessionID: "a", SessionNumber: 1},
		},
	}
	courseB := courseA
	courseB.Sessions = []domain.SessionSummary{courseA.Sessions[1], courseA.Sessions[0]}
	aJSON, aHash, err := Canonicalize(domain.SnapshotCourseDetail, &courseA, 1<<20)
	require.NoError(t, err)
	bJSON, bHash, err := Canonicalize(domain.SnapshotCourseDetail, &courseB, 1<<20)
	require.NoError(t, err)
	require.Equal(t, aJSON, bJSON)
	require.Equal(t, aHash, bHash)

	nilCourse := domain.CourseDetail{CourseSummary: domain.CourseSummary{CourseID: "empty"}}
	emptyCourse := nilCourse
	emptyCourse.Sessions = []domain.SessionSummary{}
	nilJSON, nilHash, err := Canonicalize(domain.SnapshotCourseDetail, nilCourse, 1<<20)
	require.NoError(t, err)
	emptyJSON, emptyHash, err := Canonicalize(domain.SnapshotCourseDetail, emptyCourse, 1<<20)
	require.NoError(t, err)
	require.Equal(t, nilJSON, emptyJSON)
	require.Equal(t, nilHash, emptyHash)
}

func TestCanonicalizeSessionNormalizesOrderAndTimestampOffsets(t *testing.T) {
	utc := "2026-07-26T10:00:00Z"
	offset := "2026-07-26T17:00:00+07:00"
	first := domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session"},
		Students: []domain.StudentCheckin{
			{StudentID: "b", Name: "Beta"},
			{StudentID: "a", Name: "Alpha", CheckedInAt: &utc},
		},
		QRExpiresAt: &utc,
	}
	second := first
	second.Students = []domain.StudentCheckin{first.Students[1], first.Students[0]}
	second.Students[0].CheckedInAt = &offset
	second.QRExpiresAt = &offset

	firstJSON, firstHash, err := Canonicalize(domain.SnapshotSessionDetail, first, 1<<20)
	require.NoError(t, err)
	secondJSON, secondHash, err := Canonicalize(domain.SnapshotSessionDetail, second, 1<<20)
	require.NoError(t, err)
	require.Equal(t, firstJSON, secondJSON)
	require.Equal(t, firstHash, secondHash)
}

func TestCanonicalizeProfilesAndTypeSafety(t *testing.T) {
	profiles := []domain.StudentProfile{
		{StudentGuid: "b", StudentID: "2"},
		{StudentGuid: "a", StudentID: "9"},
		{StudentGuid: "a", StudentID: "1"},
	}
	firstJSON, firstHash, err := Canonicalize(domain.SnapshotStudentProfiles, profiles, 1<<20)
	require.NoError(t, err)
	secondJSON, secondHash, err := Canonicalize(domain.SnapshotStudentProfiles,
		[]domain.StudentProfile{profiles[2], profiles[0], profiles[1]}, 1<<20)
	require.NoError(t, err)
	require.Equal(t, firstJSON, secondJSON)
	require.Equal(t, firstHash, secondHash)

	_, _, err = Canonicalize(domain.SnapshotStudentProfiles, domain.CourseDetail{}, 1<<20)
	require.Error(t, err)
	var typedNil *domain.CourseDetail
	_, _, err = Canonicalize(domain.SnapshotCourseDetail, typedNil, 1<<20)
	require.Error(t, err)
}

func TestCanonicalizeEnforcesPayloadCeilingWithoutTruncation(t *testing.T) {
	value := []domain.CourseSummary{{CourseID: "course", Name: "A long course name"}}
	payload, _, err := Canonicalize(domain.SnapshotCourseCatalog, value, 8)
	require.Error(t, err)
	require.Nil(t, payload)
}

func TestCanonicalizeAtoBtoAHash(t *testing.T) {
	a := []domain.CourseSummary{{CourseID: "a"}}
	b := []domain.CourseSummary{{CourseID: "b"}}
	_, hashA1, err := Canonicalize(domain.SnapshotCourseCatalog, a, 1<<20)
	require.NoError(t, err)
	_, hashB, err := Canonicalize(domain.SnapshotCourseCatalog, b, 1<<20)
	require.NoError(t, err)
	_, hashA2, err := Canonicalize(domain.SnapshotCourseCatalog, a, 1<<20)
	require.NoError(t, err)
	require.Equal(t, hashA1, hashA2)
	require.NotEqual(t, hashA1, hashB)
}

func TestNormalizeRFC3339LeavesNonTimestampStringsStable(t *testing.T) {
	require.Equal(t, "not-a-time", normalizeTimestampString("not-a-time"))
	value := time.Date(2026, 7, 26, 10, 0, 0, 123456789, time.UTC)
	require.Equal(t, value.Format(time.RFC3339Nano), normalizeTimestampString(value.Format(time.RFC3339Nano)))
}
