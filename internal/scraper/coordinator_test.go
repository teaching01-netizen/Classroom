package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/warwick"
)

type coordinatorSource struct {
	result warwick.SnapshotFetchResult
	err    error
	calls  int
	fetch  func(context.Context, domain.ScrapeTarget) (warwick.SnapshotFetchResult, error)
}

func (s *coordinatorSource) Fetch(
	ctx context.Context,
	target domain.ScrapeTarget,
) (warwick.SnapshotFetchResult, error) {
	s.calls++
	if s.fetch != nil {
		return s.fetch(ctx, target)
	}
	return s.result, s.err
}

type coordinatorStore struct {
	inputs      []db.CommitInput
	releases    []db.ReleaseLeaseRequest
	commitErr   error
	nextRunID   int64
	nextVersion int64
}

func (s *coordinatorStore) Commit(_ context.Context, input db.CommitInput) (db.CommitResult, error) {
	s.inputs = append(s.inputs, input)
	if s.commitErr != nil {
		return db.CommitResult{}, s.commitErr
	}
	s.nextRunID++
	result := db.CommitResult{RunID: s.nextRunID}
	if input.Changed {
		s.nextVersion++
		result.Snapshot = &domain.Snapshot{Version: s.nextVersion}
		result.Metadata = &domain.SnapshotMetadata{Version: s.nextVersion}
	}
	return result, nil
}

func (s *coordinatorStore) ReleaseLease(_ context.Context, request db.ReleaseLeaseRequest) error {
	s.releases = append(s.releases, request)
	return nil
}

type coordinatorObserver struct {
	observations []domain.HostObservation
	err          error
}

func (o *coordinatorObserver) Observe(_ context.Context, observation domain.HostObservation) error {
	o.observations = append(o.observations, observation)
	return o.err
}

func coordinatorTarget(kind domain.SnapshotKind) domain.ScrapeTarget {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	policy := PolicyFor(kind, domain.SessionStatusActive)
	resourceKey := "resource-1"
	if kind == domain.SnapshotCourseCatalog {
		resourceKey = "catalog"
	}
	return domain.ScrapeTarget{
		ID: 1,
		Ref: domain.TargetRef{
			Host: "warwick.humantix.cloud", Kind: kind,
			ResourceKey: resourceKey,
		},
		Attributes:      json.RawMessage(`{}`),
		CurrentInterval: policy.Initial,
		MinInterval:     policy.Min,
		MaxInterval:     policy.Max,
		MaxServeAge:     policy.MaxServeAge,
		NextRunAt:       now,
		LeaseOwner:      "worker",
		LeaseGeneration: 1,
	}
}

func newCoordinatorForTest(source *coordinatorSource, store *coordinatorStore, observer *coordinatorObserver, now time.Time) *Coordinator {
	return NewCoordinator(source, store, observer, CoordinatorConfig{
		FetchTimeout:          30 * time.Second,
		CanonicalPayloadLimit: 1 << 20,
		Clock:                 func() time.Time { return now },
		Random:                rand.New(rand.NewSource(7)),
	})
}

func TestCoordinatorChangedCatalogDiscoversCourses(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: []domain.CourseSummary{
			{CourseID: "b", Name: "Beta", Status: domain.CourseStatusUpcoming},
			{CourseID: "a", Name: "Alpha", Status: domain.CourseStatusActive},
		},
		Metadata:  warwick.ResponseMetadata{StatusCode: 200, ETag: `"v1"`},
		BytesRead: 100,
	}}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)

	result, err := coordinator.RunClaimed(context.Background(), coordinatorTarget(domain.SnapshotCourseCatalog))
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, "changed", result.Outcome)
	require.Len(t, store.inputs, 1)
	input := store.inputs[0]
	require.True(t, input.Changed)
	require.Len(t, input.Discovered, 2)
	require.Equal(t, "a", input.Discovered[0].Ref.ResourceKey)
	require.Equal(t, domain.SnapshotCourseDetail, input.Discovered[0].Ref.Kind)
	require.Contains(t, string(input.Discovered[0].Attributes), "course_name")
	require.Len(t, input.SeenChildRefs, 2)
	require.Equal(t, `"v1"`, input.ETag)
	require.Len(t, observer.observations, 1)
	require.Equal(t, "changed", observer.observations[0].Outcome)
}

func TestCoordinatorIdenticalContentIsUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	value := []domain.CourseSummary{{CourseID: "a", Name: "Alpha"}}
	payload, hash, err := Canonicalize(domain.SnapshotCourseCatalog, value, 1<<20)
	require.NoError(t, err)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentContentHash = hash
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: value, BytesRead: int64(len(payload))}}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, "unchanged", result.Outcome)
	require.False(t, store.inputs[0].Changed)
	require.Nil(t, store.inputs[0].Payload)
	require.Equal(t, int64(2), *store.inputs[0].ValidationSeqAfter)
}

func TestCoordinatorNotModifiedAdvancesValidationOnly(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 4
	source := &coordinatorSource{
		result: warwick.SnapshotFetchResult{Metadata: warwick.ResponseMetadata{
			StatusCode: 304, ETag: `"v1"`,
		}},
		err: domain.ErrNotModified,
	}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "not_modified", result.Outcome)
	require.False(t, store.inputs[0].Changed)
	require.Equal(t, int64(5), *store.inputs[0].ValidationSeqAfter)
	require.Equal(t, `"v1"`, store.inputs[0].ETag)
}

func TestCoordinatorRejectsNotModifiedWithoutCurrentSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{err: domain.ErrNotModified}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(context.Background(), coordinatorTarget(domain.SnapshotCourseCatalog))
	require.NoError(t, err)
	require.Equal(t, "invalid_payload", result.Outcome)
	require.Equal(t, "invalid_payload", store.inputs[0].Outcome)
	require.Nil(t, store.inputs[0].ValidationSeqAfter)
}

func TestCoordinatorFailureRetainsValidationAndSanitizesError(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{err: errors.New(
		"Cookie: cookie-secret; ASP.NET_SessionId=session-secret; " +
			"Password=password-secret; Authorization: Bearer auth-secret",
	)}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	target := coordinatorTarget(domain.SnapshotSessionDetail)
	target.HasCurrentSnapshot = true
	target.ValidationSeq = 3

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "transient_error", result.Outcome)
	require.Nil(t, store.inputs[0].ValidationSeqAfter)
	require.False(t, store.inputs[0].Changed)
	for _, secret := range []string{
		"cookie-secret",
		"session-secret",
		"password-secret",
		"auth-secret",
	} {
		require.NotContains(t, store.inputs[0].ErrorMessage, secret)
	}
	require.Contains(t, store.inputs[0].ErrorMessage, "<redacted>")
	require.Equal(t, 1, store.inputs[0].ConsecutiveFailures)
}

func TestCoordinatorUnknownKindSkipsSource(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.Ref.Kind = "unknown"

	_, err := coordinator.RunClaimed(context.Background(), target)
	require.Error(t, err)
	require.Zero(t, source.calls)
	require.Empty(t, store.inputs)
}

func TestCoordinatorPropagatesLeaseLoss(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: []domain.CourseSummary{}}}
	store := &coordinatorStore{commitErr: domain.ErrLeaseLost}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	_, err := coordinator.RunClaimed(context.Background(), coordinatorTarget(domain.SnapshotCourseCatalog))
	require.ErrorIs(t, err, domain.ErrLeaseLost)
}

func TestCoordinatorReleasesPermitHookBeforeCanonicalizationAndCommit(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: "wrong type"}}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)
	released := false

	_, err := coordinator.RunClaimedWithRelease(
		context.Background(),
		coordinatorTarget(domain.SnapshotCourseCatalog),
		func() { released = true },
	)
	require.NoError(t, err)
	require.True(t, released)
	require.Len(t, store.inputs, 1)
	require.Equal(t, "invalid_payload", store.inputs[0].Outcome)
}

func TestCoordinatorCanceledFetchReleasesLeaseWithoutFailureRun(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{err: context.Canceled}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)

	_, err := coordinator.RunClaimed(context.Background(), target)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, store.inputs)
	require.Equal(t, []db.ReleaseLeaseRequest{{
		TargetID: target.ID, LeaseGeneration: target.LeaseGeneration,
	}}, store.releases)
}

func TestCoordinatorCancellationMidFetchReleasesLeaseAndDoesNotCommit(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	source := &coordinatorSource{
		fetch: func(ctx context.Context, _ domain.ScrapeTarget) (warwick.SnapshotFetchResult, error) {
			close(started)
			<-ctx.Done()
			return warwick.SnapshotFetchResult{}, ctx.Err()
		},
	}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, runErr := coordinator.RunClaimed(ctx, target)
		done <- runErr
	}()
	<-started
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	require.Empty(t, store.inputs)
	require.Empty(t, observer.observations)
	require.Equal(t, []db.ReleaseLeaseRequest{{
		TargetID: target.ID, LeaseGeneration: target.LeaseGeneration,
	}}, store.releases)
}

func TestCoordinatorOversizedCanonicalPayloadIsInvalidAndObservedOnce(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: []domain.CourseSummary{{
			CourseID: "course-1",
			Name:     "payload larger than the deliberately tiny ceiling",
		}},
	}}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)
	coordinator.canonicalPayloadLimit = 8

	result, err := coordinator.RunClaimed(
		context.Background(),
		coordinatorTarget(domain.SnapshotCourseCatalog),
	)
	require.NoError(t, err)
	require.Equal(t, "invalid_payload", result.Outcome)
	require.False(t, result.Succeeded)
	require.Len(t, store.inputs, 1)
	require.False(t, store.inputs[0].Changed)
	require.Nil(t, store.inputs[0].ValidationSeqAfter)
	require.Len(t, observer.observations, 1)
	require.Equal(t, "invalid_payload", observer.observations[0].Outcome)
}

func TestCoordinatorNotFoundUsesDeterministicLongPauseAndRetainsCurrent(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{
		result: warwick.SnapshotFetchResult{
			Metadata: warwick.ResponseMetadata{StatusCode: 404},
		},
		err: &domain.UpstreamStatusError{StatusCode: 404},
	}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)
	target := coordinatorTarget(domain.SnapshotSessionDetail)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 3
	target.ValidationSeq = 7

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "not_found", result.Outcome)
	require.Equal(t, now.Add(24*time.Hour), result.NextRunAt)
	require.False(t, result.Changed)
	require.Nil(t, store.inputs[0].ValidationSeqAfter)
	require.Equal(t, target.ValidationSeq, int64(7))
	require.Len(t, observer.observations, 1)
}

func TestCoordinatorTransientRetryAfterWinsOverLocalJitter(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{
		result: warwick.SnapshotFetchResult{
			Metadata: warwick.ResponseMetadata{StatusCode: 503},
		},
		err: &domain.UpstreamStatusError{
			StatusCode: 503,
			RetryAfter: 20 * time.Minute,
		},
	}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(
		source,
		store,
		&coordinatorObserver{},
		now,
	)

	result, err := coordinator.RunClaimed(
		context.Background(),
		coordinatorTarget(domain.SnapshotSessionDetail),
	)

	require.NoError(t, err)
	require.Equal(t, "transient_error", result.Outcome)
	require.Equal(t, now.Add(20*time.Minute), result.NextRunAt)
	require.Equal(t, result.NextRunAt, store.inputs[0].NextRunAt)
}

func TestCoordinatorChangedCourseDiscoversSessionPolicies(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	detail := &domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "course-1", Name: "Math"},
		Sessions: []domain.SessionSummary{
			{SessionID: "finished", Status: domain.SessionStatusDone},
			{SessionID: "active", Status: domain.SessionStatusActive},
		},
	}
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: detail}}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	target := coordinatorTarget(domain.SnapshotCourseDetail)
	target.Ref.ResourceKey = "course-1"

	_, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Len(t, store.inputs[0].Discovered, 2)
	for _, seed := range store.inputs[0].Discovered {
		require.Equal(t, "course-1", seed.Ref.ParentKey)
		if seed.Ref.ResourceKey == "finished" {
			require.Equal(t, 12*time.Hour, seed.InitialInterval)
		}
	}
}

func TestCoordinatorCanonicalHashMatchesCommittedBytes(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	value := []domain.CourseSummary{{CourseID: "a"}}
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: value}}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)
	_, err := coordinator.RunClaimed(context.Background(), coordinatorTarget(domain.SnapshotCourseCatalog))
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(store.inputs[0].Payload), store.inputs[0].ContentHash)
}
