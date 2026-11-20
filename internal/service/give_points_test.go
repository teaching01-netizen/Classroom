package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

// givePointsProviderFake is a live-mode provider that can award points.
type givePointsProviderFake struct {
	*mockProvider

	mu           sync.Mutex
	points       int
	canGive      bool
	haveTarget   bool
	targetCourse string
	clampTo      int
	awarded      int
	awardedTo    string
	awardCount   int
	awardErr     error
	targetCalls  int
}

func newGivePointsProviderFake(points int, canGive bool) *givePointsProviderFake {
	return &givePointsProviderFake{
		mockProvider: newMockProvider(),
		points:       points,
		canGive:      canGive,
		haveTarget:   true,
		targetCourse: "2317",
	}
}

func (p *givePointsProviderFake) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1"},
		Students: []domain.StudentCheckin{{
			StudentID:           "guid-a",
			CheckedIn:           true,
			ParticipationPoints: p.points,
			CanGivePoint:        p.canGive,
		}},
	}, nil
}

func (p *givePointsProviderFake) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return []domain.StudentProfile{{StudentID: "W123", StudentGuid: "guid-a"}}, nil
}

func (p *givePointsProviderFake) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func (p *givePointsProviderFake) FetchGivePointTarget(context.Context, string) (domain.GivePointTarget, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.targetCalls++
	if !p.haveTarget {
		return domain.GivePointTarget{}, domain.ErrGivePointTargetUnavailable
	}
	return domain.GivePointTarget{AttendanceID: "67", WarwickCourseID: p.targetCourse}, nil
}

func (p *givePointsProviderFake) AddParticipationPoint(_ context.Context, _ domain.GivePointTarget, studentGUID string, points int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.awardErr != nil {
		return p.awardErr
	}
	if p.clampTo > 0 && points > p.clampTo {
		points = p.clampTo
	}
	p.awarded = points
	p.awardedTo = studentGUID
	p.awardCount++
	p.points += points
	return nil
}

func TestGivePointsAwardsAndReturnsNewTotal(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	mutator := newCheckinMutatorFake()
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, mutator, NoopSnapshotRefresher{}, 2, false,
	)

	result, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID:  "2317",
		SessionID: "20518",
		StudentID: "W123",
		Points:    2,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, result.PointsAwarded)
	assert.Equal(t, 6, result.ParticipationPoints)
	assert.Equal(t, 2, provider.awarded)
	assert.Equal(t, "guid-a", provider.awardedTo)
	assert.Equal(t, 1, mutator.advisorLockCalled)
}

func TestGivePointsRejectsWhenWarwickSaysNotEligible(t *testing.T) {
	provider := newGivePointsProviderFake(0, false)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	var ineligible *ErrPointsNotEligible
	require.ErrorAs(t, err, &ineligible)
	assert.Zero(t, provider.awarded, "no upstream write may happen when ineligible")
}

func TestGivePointsRejectsStaleExpectedPoints(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)
	expected := 3

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &expected,
	})

	var conflict *ErrPointsConflict
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 4, conflict.CurrentPoints)
	assert.Zero(t, provider.awarded)
}

func TestGivePointsValidatesRange(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	for _, points := range []int{0, -1, 11, 1000} {
		_, err := svc.GivePoints(context.Background(), GivePointsRequest{
			CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: points,
		})
		var invalid *ErrInvalidPoints
		require.ErrorAs(t, err, &invalid, "points=%d must be rejected", points)
	}
}

func TestGivePointsFailsClosedWhenTargetCannotBeScraped(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	provider.haveTarget = false
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	require.ErrorIs(t, err, domain.ErrGivePointTargetUnavailable)
	assert.Zero(t, provider.awarded)
}

// TestGivePointsRejectsCrossCourseTarget proves the scraped page cannot award
// into a course other than the one in the route.
func TestGivePointsRejectsCrossCourseTarget(t *testing.T) {
	provider := newGivePointsProviderFake(0, true)
	provider.targetCourse = "9999"
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	_, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1,
	})

	var mismatch *ErrPointsTargetMismatch
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, "9999", mismatch.PageCourseID)
	assert.Equal(t, "2317", mismatch.RouteCourseID)
	assert.Zero(t, provider.awardCount, "a cross-course target must never be written")
}

// TestGivePointsSecondAwardWithStaleTotalIsRejected proves the client-visible
// total is a real concurrency token: a repeated submit carrying the total the
// teacher already saw is refused instead of awarding twice.
func TestGivePointsSecondAwardWithStaleTotalIsRejected(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)
	seen := 4

	first, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &seen,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, first.ParticipationPoints)

	_, err = svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 1, ExpectedPoints: &seen,
	})

	var conflict *ErrPointsConflict
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, 5, conflict.CurrentPoints)
	assert.Equal(t, 1, provider.awardCount, "the stale retry must not write again")
}

// TestGivePointsResultReflectsAuthoritativeTotal proves the response total is
// re-read from the roster, not computed as before+requested. The fake clamps
// the award the way an upstream business rule would.
func TestGivePointsResultReflectsAuthoritativeTotal(t *testing.T) {
	provider := newGivePointsProviderFake(4, true)
	provider.clampTo = 5
	svc := NewTeacherServiceWithDependenciesAndMutator(
		provider, provider, provider, newCheckinMutatorFake(), NoopSnapshotRefresher{}, 2, false,
	)

	result, err := svc.GivePoints(context.Background(), GivePointsRequest{
		CourseID: "2317", SessionID: "20518", StudentID: "W123", Points: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, 10, result.PointsAwarded)
	assert.Equal(t, 9, result.ParticipationPoints, "4 + clamped 5, not 4 + requested 10")
}
