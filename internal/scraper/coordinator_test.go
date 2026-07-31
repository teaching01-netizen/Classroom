package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
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

// coordinatorConfirmingSource implements ConfirmationSource on top of a
// coordinatorSource so tests can exercise the confirmation flow.
type coordinatorConfirmingSource struct {
	coordinatorSource
	confirmation warwick.SnapshotFetchResult
	confirmErr   error
	confirmCalls int
}

func (s *coordinatorConfirmingSource) FetchConfirmation(
	_ context.Context,
	_ domain.ScrapeTarget,
) (warwick.SnapshotFetchResult, error) {
	s.confirmCalls++
	return s.confirmation, s.confirmErr
}

type coordinatorStore struct {
	inputs                []db.CommitInput
	releases              []db.ReleaseLeaseRequest
	lifecycleInputs       []db.LifecycleReconcileInput
	commitErr             error
	reconcileLifecycleErr error
	nextRunID             int64
	nextVersion           int64
	current               domain.Snapshot
	currentErr            error
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

// Current returns the configured previous snapshot, or ErrSnapshotNotFound
// when none was set so runs behave as if no snapshot exists.
func (s *coordinatorStore) Current(_ context.Context, _ domain.TargetRef) (domain.Snapshot, error) {
	if s.currentErr != nil {
		return domain.Snapshot{}, s.currentErr
	}
	if s.current.Version == 0 {
		return domain.Snapshot{}, domain.ErrSnapshotNotFound
	}
	return s.current, nil
}

func (s *coordinatorStore) ReleaseLease(_ context.Context, request db.ReleaseLeaseRequest) error {
	s.releases = append(s.releases, request)
	return nil
}

func (s *coordinatorStore) RenewLease(_ context.Context, _ db.RenewLeaseRequest) error {
	return nil
}

func (s *coordinatorStore) ReconcileLifecycle(_ context.Context, input db.LifecycleReconcileInput) error {
	s.lifecycleInputs = append(s.lifecycleInputs, input)
	return s.reconcileLifecycleErr
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
	return newCoordinatorForTestAny(source, store, observer, now)
}

func newCoordinatorForTestAny(source Source, store *coordinatorStore, observer *coordinatorObserver, now time.Time) *Coordinator {
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
	payload, hash, _, err := Canonicalize(domain.SnapshotCourseCatalog, value, 1<<20)
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

func TestCoordinatorSuspiciousOutcomeQuarantinesWithoutConfirmationSource(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	// Second fetch returns only 2 courses (large drop from 50).
	smallCatalog := []domain.CourseSummary{
		{CourseID: "course-a", Name: "Course A", Status: domain.CourseStatusActive},
		{CourseID: "course-b", Name: "Course B", Status: domain.CourseStatusActive},
	}
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: smallCatalog,
		Metadata: warwick.ResponseMetadata{
			StatusCode: 200,
			ETag:       `"v2"`,
		},
		BytesRead: 100,
	}}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	// Set up target with existing snapshot.
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	target.PreviousRecordCount = 50

	// A source without FetchConfirmation cannot confirm the suspicious drop,
	// so the candidate is quarantined and the last-known-good is preserved.
	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "quarantined", result.Outcome)
	require.False(t, result.Changed)
	require.Len(t, store.inputs, 1)
	require.False(t, store.inputs[0].Changed)
	require.Nil(t, store.inputs[0].Payload)
	require.Len(t, store.inputs[0].Candidates, 1)
	require.Equal(t, domain.CandidateQuarantinedAnomaly, store.inputs[0].Candidates[0].Disposition)
	require.Equal(t, "confirmation_unavailable", store.inputs[0].Candidates[0].RejectionCode)
	require.Equal(t, "confirmation_unavailable", store.inputs[0].LastRejectionCode)
}

func TestCoordinatorConfirmationConsistentPublishesChange(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	smallCatalog := []domain.CourseSummary{
		{CourseID: "course-a", Name: "Course A", Status: domain.CourseStatusActive},
		{CourseID: "course-b", Name: "Course B", Status: domain.CourseStatusActive},
	}
	source := &coordinatorConfirmingSource{
		coordinatorSource: coordinatorSource{result: warwick.SnapshotFetchResult{
			Value: smallCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: strings.Repeat("A", 64),
			},
			BytesRead: 100,
		}},
		confirmation: warwick.SnapshotFetchResult{
			Value: smallCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: strings.Repeat("A", 64),
			},
			BytesRead: 100,
		},
	}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTestAny(source, store, &coordinatorObserver{}, now)

	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	target.PreviousRecordCount = 50

	// The independent confirmation fetch matches (same raw hash, same
	// canonical hash), so the change is published.
	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "changed", result.Outcome)
	require.True(t, result.Changed)
	require.Equal(t, 1, source.confirmCalls)
	require.Len(t, store.inputs, 1)
	require.True(t, store.inputs[0].Changed)
	require.NotNil(t, store.inputs[0].Payload)
	require.Len(t, store.inputs[0].Candidates, 1)
	require.Equal(t, domain.CandidateAccepted, store.inputs[0].Candidates[0].Disposition)
	require.NotEmpty(t, store.inputs[0].Candidates[0].ConfirmationGroupUUID)
	require.True(t, store.inputs[0].Manifest.Complete)
}

func TestCoordinatorConfirmationMismatchQuarantinesBothCandidates(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	smallCatalog := []domain.CourseSummary{
		{CourseID: "course-a", Name: "Course A", Status: domain.CourseStatusActive},
		{CourseID: "course-b", Name: "Course B", Status: domain.CourseStatusActive},
	}
	differentCatalog := []domain.CourseSummary{
		{CourseID: "course-x", Name: "Course X", Status: domain.CourseStatusActive},
		{CourseID: "course-y", Name: "Course Y", Status: domain.CourseStatusActive},
	}
	source := &coordinatorConfirmingSource{
		coordinatorSource: coordinatorSource{result: warwick.SnapshotFetchResult{
			Value: smallCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: strings.Repeat("A", 64),
			},
			BytesRead: 100,
		}},
		confirmation: warwick.SnapshotFetchResult{
			Value: differentCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: strings.Repeat("B", 64),
			},
			BytesRead: 100,
		},
	}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTestAny(source, store, &coordinatorObserver{}, now)

	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	target.PreviousRecordCount = 50

	// The confirmation fetch disagrees with the original fetch on both the
	// raw body and the canonical payload, so both candidates are
	// quarantined under one confirmation group.
	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "quarantined", result.Outcome)
	require.False(t, result.Changed)
	require.Equal(t, 1, source.confirmCalls)
	require.Len(t, store.inputs, 1)
	require.Len(t, store.inputs[0].Candidates, 2)
	var first, second domain.ScrapeCandidate
	for _, candidate := range store.inputs[0].Candidates {
		switch candidate.AttemptNumber {
		case 1:
			first = candidate
		case 2:
			second = candidate
		default:
			t.Fatalf("unexpected attempt number %d", candidate.AttemptNumber)
		}
	}
	require.NotEmpty(t, first.ConfirmationGroupUUID)
	require.Equal(t, first.ConfirmationGroupUUID, second.ConfirmationGroupUUID)
	for _, candidate := range store.inputs[0].Candidates {
		require.Equal(t, domain.CandidateQuarantinedAnomaly, candidate.Disposition)
		require.Equal(t, "confirmation_mismatch", candidate.RejectionCode)
		require.Len(t, candidate.CanonicalHash, 64)
	}
	require.NotEqual(t, first.CanonicalHash, second.CanonicalHash)
	require.Equal(t, "confirmation_mismatch", store.inputs[0].LastRejectionCode)
}

func TestCoordinatorRawHashFastPathSkipsParseAndCommitsUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	rawHash := strings.Repeat("ab", 32)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		// This value would fail validation if the fast path parsed it.
		Value: "wrong type for catalog",
		Metadata: warwick.ResponseMetadata{
			StatusCode:  200,
			RawBodyHash: rawHash,
		},
		BytesRead: 100,
	}}
	store := &coordinatorStore{current: domain.Snapshot{
		Version:       1,
		ParserVersion: ParserVersion,
		RawBodyHash:   rawHash,
		Payload:       json.RawMessage(`[{"course_id":"a"}]`),
	}}
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "unchanged", result.Outcome)
	require.False(t, result.Changed)
	require.Len(t, store.inputs, 1)
	require.False(t, store.inputs[0].Changed)
	require.Equal(t, 0, store.inputs[0].RecordsCount)
	require.Nil(t, store.inputs[0].Payload)
	require.Zero(t, store.inputs[0].ContentHash)
	require.Equal(t, int64(2), *store.inputs[0].ValidationSeqAfter)
	require.Len(t, store.inputs[0].Candidates, 1)
	candidate := store.inputs[0].Candidates[0]
	require.Equal(t, domain.CandidateUnchanged, candidate.Disposition)
	require.Nil(t, candidate.Payload)
	require.Zero(t, candidate.Manifest.Complete)
	require.Equal(t, rawHash, candidate.RawBodyHash)
}

func TestCoordinatorRawHashDiffersTakesNormalParsePath(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	value := []domain.CourseSummary{{
		CourseID: "a", Name: "Alpha", Status: domain.CourseStatusActive,
	}}
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: value,
		Metadata: warwick.ResponseMetadata{
			StatusCode:  200,
			RawBodyHash: strings.Repeat("cd", 32),
		},
		BytesRead: 100,
	}}
	store := &coordinatorStore{current: domain.Snapshot{
		Version:       1,
		ParserVersion: ParserVersion,
		RawBodyHash:   strings.Repeat("ab", 32),
		Payload:       json.RawMessage(`[{"course_id":"a"}]`),
	}}
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "changed", result.Outcome)
	require.True(t, result.Changed)
	require.Len(t, store.inputs, 1)
	require.True(t, store.inputs[0].Changed)
	require.NotNil(t, store.inputs[0].Payload)
	require.Equal(t, 1, store.inputs[0].RecordsCount)
}

func TestCoordinatorPreviousSnapshotIDsTriggerSuspicion(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	previous := make([]domain.CourseSummary, 0, 10)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		previous = append(previous, domain.CourseSummary{CourseID: id})
	}
	prevPayload, _, prevCount, err := Canonicalize(domain.SnapshotCourseCatalog, previous, 1<<20)
	require.NoError(t, err)
	// 8 records (drop 0.2, within tolerance) but only 3 of the previous IDs
	// survive (missing ratio 0.7): only the ID-based rule can flag this.
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: []domain.CourseSummary{
			{CourseID: "a"}, {CourseID: "b"}, {CourseID: "c"},
			{CourseID: "k"}, {CourseID: "l"}, {CourseID: "m"},
			{CourseID: "n"}, {CourseID: "o"},
		},
		Metadata:  warwick.ResponseMetadata{StatusCode: 200},
		BytesRead: 100,
	}}
	store := &coordinatorStore{current: domain.Snapshot{
		Version:       1,
		ParserVersion: ParserVersion,
		Payload:       prevPayload,
	}}
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	target.PreviousRecordCount = prevCount
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	// No ConfirmationSource, so the suspicious candidate is quarantined.
	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "quarantined", result.Outcome)
	require.False(t, result.Changed)
	require.Len(t, store.inputs, 1)
	require.Len(t, store.inputs[0].Candidates, 1)
	require.Equal(t, domain.CandidateQuarantinedAnomaly, store.inputs[0].Candidates[0].Disposition)
	require.Equal(t, "confirmation_unavailable", store.inputs[0].Candidates[0].RejectionCode)
}

func TestCoordinatorConfirmationSameRawDifferentCanonicalQuarantinesNondeterminism(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	smallCatalog := []domain.CourseSummary{
		{CourseID: "course-a", Name: "Course A", Status: domain.CourseStatusActive},
		{CourseID: "course-b", Name: "Course B", Status: domain.CourseStatusActive},
	}
	differentCatalog := []domain.CourseSummary{
		{CourseID: "course-x", Name: "Course X", Status: domain.CourseStatusActive},
		{CourseID: "course-y", Name: "Course Y", Status: domain.CourseStatusActive},
	}
	rawHash := strings.Repeat("A", 64)
	source := &coordinatorConfirmingSource{
		coordinatorSource: coordinatorSource{result: warwick.SnapshotFetchResult{
			Value: smallCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: rawHash,
			},
			BytesRead: 100,
		}},
		confirmation: warwick.SnapshotFetchResult{
			Value: differentCatalog,
			Metadata: warwick.ResponseMetadata{
				StatusCode:  200,
				RawBodyHash: rawHash,
			},
			BytesRead: 100,
		},
	}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTestAny(source, store, &coordinatorObserver{}, now)

	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentVersion = 1
	target.ValidationSeq = 1
	target.PreviousRecordCount = 50

	// Same raw bytes, different canonical hash: the parser is nondeterministic.
	result, err := coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "quarantined", result.Outcome)
	require.False(t, result.Changed)
	require.Equal(t, 1, source.confirmCalls)
	require.Len(t, store.inputs, 1)
	require.Len(t, store.inputs[0].Candidates, 2)
	for _, candidate := range store.inputs[0].Candidates {
		require.Equal(t, domain.CandidateQuarantinedAnomaly, candidate.Disposition)
		require.Equal(t, "confirmation_parser_nondeterminism", candidate.RejectionCode)
		require.Len(t, candidate.CanonicalHash, 64)
	}
	require.Equal(t, "confirmation_parser_nondeterminism", store.inputs[0].LastRejectionCode)
}

func TestCoordinatorReconcileLifecycleCalledAfterSuccessfulCommit(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: []domain.CourseSummary{
			{CourseID: "a", Name: "Alpha", Status: domain.CourseStatusActive},
		},
		Metadata:  warwick.ResponseMetadata{StatusCode: 200},
		BytesRead: 100,
	}}
	store := &coordinatorStore{}
	observer := &coordinatorObserver{}
	coordinator := newCoordinatorForTest(source, store, observer, now)

	result, err := coordinator.RunClaimed(
		context.Background(),
		coordinatorTarget(domain.SnapshotCourseCatalog),
	)
	require.NoError(t, err)
	require.Equal(t, "changed", result.Outcome)
	require.Len(t, store.lifecycleInputs, 1)
	input := store.lifecycleInputs[0]
	require.Equal(t, domain.SnapshotCourseCatalog, input.ParentRef.Kind)
	require.Len(t, input.DiscoveredSeeds, 1)
	require.Equal(t, "a", input.DiscoveredSeeds[0].Ref.ResourceKey)
	require.Len(t, input.SeenChildRefs, 1)
}

func TestCoordinatorReconcileLifecycleCalledOnUnchanged(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	value := []domain.CourseSummary{{CourseID: "a", Name: "Alpha"}}
	_, hash, _, err := Canonicalize(domain.SnapshotCourseCatalog, value, 1<<20)
	require.NoError(t, err)
	target := coordinatorTarget(domain.SnapshotCourseCatalog)
	target.HasCurrentSnapshot = true
	target.CurrentContentHash = hash
	target.CurrentVersion = 1
	target.ValidationSeq = 1

	source := &coordinatorSource{result: warwick.SnapshotFetchResult{Value: value, BytesRead: 100}}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	_, err = coordinator.RunClaimed(context.Background(), target)
	require.NoError(t, err)
	// Even on unchanged, ReconcileLifecycle should be called so missing
	// children are detected and tombstoned after threshold.
	require.Len(t, store.lifecycleInputs, 1)
	require.Equal(t, domain.SnapshotCourseCatalog, store.lifecycleInputs[0].ParentRef.Kind)
}

func TestCoordinatorReconcileLifecycleNotCalledOnFailure(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{err: errors.New("upstream down")}
	store := &coordinatorStore{}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	_, err := coordinator.RunClaimed(
		context.Background(),
		coordinatorTarget(domain.SnapshotCourseCatalog),
	)
	require.NoError(t, err)
	require.Empty(t, store.lifecycleInputs)
}

func TestCoordinatorReconcileLifecycleErrorDoesNotFailRun(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	source := &coordinatorSource{result: warwick.SnapshotFetchResult{
		Value: []domain.CourseSummary{
			{CourseID: "a", Name: "Alpha", Status: domain.CourseStatusActive},
		},
		Metadata:  warwick.ResponseMetadata{StatusCode: 200},
		BytesRead: 100,
	}}
	store := &coordinatorStore{reconcileLifecycleErr: errors.New("db down")}
	coordinator := newCoordinatorForTest(source, store, &coordinatorObserver{}, now)

	result, err := coordinator.RunClaimed(
		context.Background(),
		coordinatorTarget(domain.SnapshotCourseCatalog),
	)
	require.NoError(t, err)
	require.Equal(t, "changed", result.Outcome)
	require.Len(t, store.lifecycleInputs, 1)
}
