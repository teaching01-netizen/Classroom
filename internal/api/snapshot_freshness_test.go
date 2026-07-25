package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

type apiSnapshotStore struct {
	snapshot domain.Snapshot
	err      error
}

func (s apiSnapshotStore) Current(context.Context, domain.TargetRef) (domain.Snapshot, error) {
	return s.snapshot, s.err
}

func (s apiSnapshotStore) Metadata(
	_ context.Context,
	ref domain.TargetRef,
	now time.Time,
) (domain.SnapshotMetadata, error) {
	if s.err != nil {
		return domain.SnapshotMetadata{}, s.err
	}
	return domain.SnapshotMetadata{
		Kind:          ref.Kind,
		ResourceKey:   ref.ResourceKey,
		ParentKey:     ref.ParentKey,
		Version:       s.snapshot.Version,
		ValidationSeq: s.snapshot.ValidationSeq,
		ValidatedAt:   s.snapshot.ValidatedAt,
		Stale:         s.snapshot.Stale(now),
	}, nil
}

func (apiSnapshotStore) AnyOverdue(context.Context, []domain.TargetRef, time.Time) (bool, error) {
	return false, nil
}

type apiSnapshotRefresher struct{}

func (apiSnapshotRefresher) RefreshNow(context.Context, domain.TargetRef) error { return nil }
func (apiSnapshotRefresher) SetDueNow(context.Context, domain.TargetRef) error  { return nil }

type apiCheckinWriter struct{}

func (apiCheckinWriter) ToggleCheckin(context.Context, string, string, string, bool) error {
	return nil
}

func TestGetCoursesHandlerAddsSnapshotFreshnessHeaders(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	payload, err := json.Marshal([]domain.CourseSummary{{CourseID: "c1"}})
	require.NoError(t, err)
	store := apiSnapshotStore{snapshot: domain.Snapshot{
		Version:       4,
		ValidationSeq: 9,
		Payload:       payload,
		ValidatedAt:   now.Add(-time.Minute),
		NextRunAt:     now.Add(-time.Second),
		MaxServeAge:   time.Hour,
	}}
	provider := service.NewSnapshotProvider(
		store,
		apiSnapshotRefresher{},
		"warwick.humantix.cloud",
		func() time.Time { return now },
	)
	teacher := service.NewTeacherServiceWithDependencies(
		provider,
		provider,
		apiCheckinWriter{},
		apiSnapshotRefresher{},
		2,
		true,
	)
	recorder := httptest.NewRecorder()

	getCoursesHandler(teacher).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/teacher/courses", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "4", recorder.Header().Get("X-Snapshot-Version"))
	require.Equal(t, "9", recorder.Header().Get("X-Snapshot-Validation-Seq"))
	require.Equal(t, now.Add(-time.Minute).Format(time.RFC3339), recorder.Header().Get("X-Snapshot-Validated-At"))
	require.Equal(t, "true", recorder.Header().Get("X-Snapshot-Stale"))
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
}

func TestMapServiceErrorMapsSnapshotAvailabilityTo503(t *testing.T) {
	tests := []struct {
		err     error
		message string
	}{
		{err: domain.ErrSnapshotNotFound, message: "snapshot unavailable"},
		{err: domain.ErrSnapshotExpired, message: "snapshot expired"},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			require.True(t, mapServiceError(recorder, errors.Join(errors.New("read failed"), test.err)))
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.message)
		})
	}
}
