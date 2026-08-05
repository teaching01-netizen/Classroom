package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

// attendanceReportRequest builds a request with the courseId route param
// injected the way chi would populate it during routing.
func attendanceReportRequest(courseID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/teacher/courses/"+courseID+"/attendance-report", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("courseId", courseID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}

// missingCourseMetadataStore fails metadata reads for course refs while
// serving snapshot content, so the report itself succeeds but freshness
// metadata is unavailable.
type missingCourseMetadataStore struct {
	apiSnapshotStore
}

func (s missingCourseMetadataStore) Metadata(
	ctx context.Context,
	ref domain.TargetRef,
	now time.Time,
) (domain.SnapshotMetadata, error) {
	if ref.Kind == domain.SnapshotCourseDetail {
		return domain.SnapshotMetadata{}, domain.ErrSnapshotNotFound
	}
	return s.apiSnapshotStore.Metadata(ctx, ref, now)
}

func newReportFreshnessTeacher(store service.SnapshotReader, snapshotMode bool) *service.TeacherService {
	provider := service.NewSnapshotProvider(
		store,
		apiSnapshotRefresher{},
		"warwick.humantix.cloud",
		time.Now,
	)
	return service.NewTeacherServiceWithDependencies(
		provider,
		provider,
		apiCheckinWriter{},
		apiSnapshotRefresher{},
		2,
		snapshotMode,
	)
}

func TestGetCourseAttendanceReportHandler_IncludesSnapshotWhenMetadataAvailable(t *testing.T) {
	// Given: a committed course snapshot with a known validation time.
	validatedAt := time.Now().UTC().Add(-2 * time.Minute)
	payload, err := json.Marshal(domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1", Name: "Math 101"},
	})
	require.NoError(t, err)
	store := apiSnapshotStore{snapshot: domain.Snapshot{
		Version:       4,
		ValidationSeq: 9,
		Payload:       payload,
		ValidatedAt:   validatedAt,
		NextRunAt:     time.Now().UTC().Add(time.Hour),
		MaxServeAge:   time.Hour,
	}}

	// When: the attendance report is requested in snapshot mode.
	recorder := httptest.NewRecorder()
	getCourseAttendanceReportHandler(newReportFreshnessTeacher(store, true)).ServeHTTP(
		recorder,
		attendanceReportRequest("c1"),
	)

	// Then: the response carries the snapshot envelope with exact times.
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success  bool                 `json:"success"`
		Data     json.RawMessage      `json:"data"`
		Snapshot *SnapshotVersionInfo `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.NotNil(t, resp.Snapshot, "snapshot envelope must be present when metadata is available")
	require.Equal(t, validatedAt.UTC().Format(time.RFC3339), resp.Snapshot.GeneratedAt)
	require.Greater(t, resp.Snapshot.AgeSeconds, int64(0))
	require.Equal(t, int64(9), resp.Snapshot.Version)
	require.Equal(t, "9", recorder.Header().Get("X-Snapshot-Validation-Seq"))
}

func TestGetCourseAttendanceReportHandler_OmitsSnapshotWhenMetadataUnavailable(t *testing.T) {
	// Given: the course snapshot exists for the report but its metadata is
	// reported missing (e.g. the snapshot disappeared between reads).
	payload, err := json.Marshal(domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1", Name: "Math 101"},
	})
	require.NoError(t, err)
	store := missingCourseMetadataStore{apiSnapshotStore{snapshot: domain.Snapshot{
		Version:       4,
		ValidationSeq: 9,
		Payload:       payload,
		ValidatedAt:   time.Now().UTC().Add(-2 * time.Minute),
		NextRunAt:     time.Now().UTC().Add(time.Hour),
		MaxServeAge:   time.Hour,
	}}}

	// When: the attendance report is requested in snapshot mode.
	recorder := httptest.NewRecorder()
	getCourseAttendanceReportHandler(newReportFreshnessTeacher(store, true)).ServeHTTP(
		recorder,
		attendanceReportRequest("c1"),
	)

	// Then: the report succeeds with a plain response and no snapshot key.
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success  bool            `json:"success"`
		Data     json.RawMessage `json:"data"`
		Snapshot json.RawMessage `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Nil(t, resp.Snapshot, "snapshot must be absent when metadata is unavailable")
	require.NotContains(t, recorder.Body.String(), `"snapshot"`)
	require.Empty(t, recorder.Header().Get("X-Snapshot-Version"))
}

func TestGetCourseAttendanceReportHandler_OmitsSnapshotInLiveMode(t *testing.T) {
	// Given: the same committed snapshot but a live-mode service (no snapshot
	// metadata is exposed).
	payload, err := json.Marshal(domain.CourseDetail{
		CourseSummary: domain.CourseSummary{CourseID: "c1", Name: "Math 101"},
	})
	require.NoError(t, err)
	store := apiSnapshotStore{snapshot: domain.Snapshot{
		Version:       4,
		ValidationSeq: 9,
		Payload:       payload,
		ValidatedAt:   time.Now().UTC().Add(-2 * time.Minute),
		NextRunAt:     time.Now().UTC().Add(time.Hour),
		MaxServeAge:   time.Hour,
	}}

	// When: the attendance report is requested in live mode.
	recorder := httptest.NewRecorder()
	getCourseAttendanceReportHandler(newReportFreshnessTeacher(store, false)).ServeHTTP(
		recorder,
		attendanceReportRequest("c1"),
	)

	// Then: the report succeeds with a plain response and no snapshot key.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), `"snapshot"`)
}
