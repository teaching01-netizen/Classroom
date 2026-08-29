package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

type checkinAPIProvider struct {
	checkedIn bool
	noOp      bool
	writeErr  error
	studentID string
}

func (p *checkinAPIProvider) GetCourses(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (p *checkinAPIProvider) GetCourseCatalog(context.Context) ([]domain.CourseSummary, error) {
	return nil, nil
}

func (p *checkinAPIProvider) GetCourseDetail(context.Context, string) (*domain.CourseDetail, error) {
	return &domain.CourseDetail{}, nil
}

func (p *checkinAPIProvider) GetCourseDetailWithName(context.Context, string, string) (*domain.CourseDetail, error) {
	return &domain.CourseDetail{}, nil
}

func (p *checkinAPIProvider) GetSessionDetail(context.Context, string, string) (*domain.SessionDetail, error) {
	return &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1"},
		Students:       []domain.StudentCheckin{{StudentID: "guid-a", CheckedIn: p.checkedIn}},
	}, nil
}

func (p *checkinAPIProvider) FetchStudentProfiles(context.Context) ([]domain.StudentProfile, error) {
	return []domain.StudentProfile{{StudentID: "W123", StudentGuid: "guid-a"}}, nil
}

func (p *checkinAPIProvider) FetchSessionForReport(context.Context, string, string) (*domain.SessionDetail, error) {
	return p.GetSessionDetail(context.Background(), "", "")
}

func (p *checkinAPIProvider) ToggleCheckin(_ context.Context, _, _, studentID string, checked bool) error {
	p.studentID = studentID
	if p.writeErr != nil {
		return p.writeErr
	}
	if !p.noOp {
		p.checkedIn = checked
	}
	return nil
}

type checkinAPILock struct{}

func (checkinAPILock) Release(context.Context) error { return nil }

type checkinAPIMutator struct{}

func (checkinAPIMutator) ReserveIdempotencyKey(context.Context, string, string, string, string, bool, *int64) (db.IdempotencyKeyResult, error) {
	return db.IdempotencyKeyResult{}, nil
}

func (checkinAPIMutator) ConfirmIdempotencyKey(context.Context, string, json.RawMessage) error {
	return nil
}

func (checkinAPIMutator) MarkIdempotencyKeyPending(context.Context, string, json.RawMessage) error {
	return nil
}

func (checkinAPIMutator) MarkIdempotencyKeyFailed(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (checkinAPIMutator) AcquireCheckinLock(context.Context, string, string) (db.CheckinLock, error) {
	return checkinAPILock{}, nil
}

func checkinAPIRequest(studentID, body string) *http.Request {
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/teacher/courses/course-1/sessions/session-1/students/"+studentID+"/checkin",
		bytes.NewBufferString(body),
	)
	request.Header.Set("Content-Type", "application/json")
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("courseId", "course-1")
	routeContext.URLParams.Add("sessionId", "session-1")
	routeContext.URLParams.Add("studentId", studentID)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func checkinAPITeacher(provider *checkinAPIProvider) *service.TeacherService {
	return service.NewTeacherServiceWithDependenciesAndMutator(
		provider,
		provider,
		provider,
		checkinAPIMutator{},
		service.NoopSnapshotRefresher{},
		2,
		false,
	)
}

func TestIdempotentCheckinHandlerStatusContract(t *testing.T) {
	tests := []struct {
		name       string
		studentID  string
		body       string
		provider   *checkinAPIProvider
		wantStatus int
	}{
		{
			name: "confirmed check-in", studentID: "W123",
			body:     `{"checkedIn":true,"idempotencyKey":"key-confirmed"}`,
			provider: &checkinAPIProvider{}, wantStatus: http.StatusOK,
		},
		{
			name: "pending verification", studentID: "W123",
			body:     `{"checkedIn":true,"idempotencyKey":"key-pending"}`,
			provider: &checkinAPIProvider{noOp: true}, wantStatus: http.StatusAccepted,
		},
		{
			name: "unknown student", studentID: "W999",
			body:     `{"checkedIn":true,"idempotencyKey":"key-unknown"}`,
			provider: &checkinAPIProvider{}, wantStatus: http.StatusNotFound,
		},
		{
			name: "upstream rejection", studentID: "W123",
			body:     `{"checkedIn":true,"idempotencyKey":"key-upstream"}`,
			provider: &checkinAPIProvider{writeErr: errors.New("rejected")}, wantStatus: http.StatusBadGateway,
		},
		{
			name: "missing idempotency key", studentID: "W123",
			body:     `{"checkedIn":true}`,
			provider: &checkinAPIProvider{}, wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			idempotentCheckinHandler(checkinAPITeacher(tt.provider)).ServeHTTP(
				recorder,
				checkinAPIRequest(tt.studentID, tt.body),
			)
			assert.Equal(t, tt.wantStatus, recorder.Code, recorder.Body.String())
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, "guid-a", tt.provider.studentID)
				var envelope ApiResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				assert.True(t, envelope.Success)
			}
		})
	}
}
