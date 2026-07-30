package scraper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestActiveSessionClaimsBeforeProfile(t *testing.T) {
	now := time.Now().UTC()
	activeSession := domain.ScrapeTarget{
		ID:   1,
		Ref:  domain.TargetRef{Kind: domain.SnapshotSessionDetail},
		Priority: domain.PriorityActiveSession,
		NextRunAt: now,
	}
	profile := domain.ScrapeTarget{
		ID:   2,
		Ref:  domain.TargetRef{Kind: domain.SnapshotStudentProfiles},
		Priority: domain.PriorityProfilesHistorical,
		NextRunAt: now,
	}
	catalog := domain.ScrapeTarget{
		ID:   3,
		Ref:  domain.TargetRef{Kind: domain.SnapshotCourseCatalog},
		Priority: domain.PriorityCatalog,
		NextRunAt: now,
	}

	// Simulate the in-memory sort used in ClaimDue after RETURNING
	targets := []domain.ScrapeTarget{catalog, profile, activeSession}
	sortTargets(targets)
	require.Equal(t, domain.PriorityActiveSession, targets[0].Priority)
	require.Equal(t, domain.PriorityCatalog, targets[1].Priority)
	require.Equal(t, domain.PriorityProfilesHistorical, targets[2].Priority)
}

func TestDiscoveryBatchUsesJitter(t *testing.T) {
	parent := domain.ScrapeTarget{
		ID:   1,
		Ref:  domain.TargetRef{Host: "test.example.com", Kind: domain.SnapshotCourseCatalog},
		Priority: domain.PriorityCatalog,
	}
	courses := []domain.CourseSummary{
		{CourseID: "c1", Name: "Course 1"},
		{CourseID: "c2", Name: "Course 2"},
		{CourseID: "c3", Name: "Course 3"},
	}

	now := time.Now().UTC()
	seeds, seen := discoverTargets(parent, courses, now)

	require.Len(t, seeds, 3)
	require.Len(t, seen, 3)

	for _, seed := range seeds {
		// NextRunAt should be at or after now (jitter is additive)
		require.False(t, seed.NextRunAt.Before(now),
			"seed NextRunAt %v should not be before now %v", seed.NextRunAt, now)
		// Jitter should be at most 5 minutes
		require.True(t, seed.NextRunAt.Before(now.Add(5*time.Minute+time.Second)),
			"seed NextRunAt %v should be within 5 minutes of now", seed.NextRunAt)
		// All discovered course targets should have course priority
		require.Equal(t, domain.PriorityActiveCourse, seed.Priority,
			"course seed should have active course priority")
	}
}

func TestDiscoveryBatchSessionPriority(t *testing.T) {
	parent := domain.ScrapeTarget{
		ID:   1,
		Ref:  domain.TargetRef{Host: "test.example.com", Kind: domain.SnapshotCourseDetail, ResourceKey: "c1"},
		Priority: domain.PriorityActiveCourse,
	}
	detail := domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1", Name: "Course 1"},
		Sessions: []domain.SessionSummary{
			{SessionID: "s1", Status: domain.SessionStatusActive},
			{SessionID: "s2", Status: domain.SessionStatusDone},
			{SessionID: "s3", Status: domain.SessionStatusNotStarted},
		},
	}

	now := time.Now().UTC()
	seeds, _ := discoverTargets(parent, detail, now)
	require.Len(t, seeds, 3)

	priorities := make(map[string]int)
	for _, seed := range seeds {
		priorities[seed.Ref.ResourceKey] = seed.Priority
	}

	require.Equal(t, domain.PriorityActiveSession, priorities["s1"],
		"active session should have active priority")
	require.Equal(t, domain.PriorityRecentlyCompleted, priorities["s2"],
		"done session should have recently completed priority")
	require.Equal(t, domain.PriorityActiveSession, priorities["s3"],
		"not_started session should have active priority")
}

func TestDefaultPriority(t *testing.T) {
	tests := []struct {
		kind   domain.SnapshotKind
		status domain.SessionStatus
		want   int
	}{
		{domain.SnapshotSessionDetail, domain.SessionStatusActive, domain.PriorityActiveSession},
		{domain.SnapshotSessionDetail, domain.SessionStatusDone, domain.PriorityRecentlyCompleted},
		{domain.SnapshotSessionDetail, domain.SessionStatusNotStarted, domain.PriorityActiveSession},
		{domain.SnapshotCourseDetail, "", domain.PriorityActiveCourse},
		{domain.SnapshotCourseCatalog, "", domain.PriorityCatalog},
		{domain.SnapshotStudentProfiles, "", domain.PriorityProfilesHistorical},
	}
	for _, tt := range tests {
		got := domain.DefaultPriority(tt.kind, tt.status)
		require.Equal(t, tt.want, got, "DefaultPriority(%s, %s)", tt.kind, tt.status)
	}
}

// sortTargets mirrors the in-memory sort used by ClaimDue.
func sortTargets(targets []domain.ScrapeTarget) {
	for i := 0; i < len(targets); i++ {
		for j := i + 1; j < len(targets); j++ {
			if !targetLess(targets[i], targets[j]) {
				targets[i], targets[j] = targets[j], targets[i]
			}
		}
	}
}

func targetLess(a, b domain.ScrapeTarget) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if a.NextRunAt.Equal(b.NextRunAt) {
		return a.ID < b.ID
	}
	return a.NextRunAt.Before(b.NextRunAt)
}
