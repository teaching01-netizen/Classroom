package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

// mapServiceError maps domain errors to HTTP status codes.
func mapServiceError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, domain.ErrAuthExpired) {
		writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
		return true
	}
	if errors.Is(err, domain.ErrPoolExhausted) {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
		return true
	}
	if errors.Is(err, domain.ErrAuthConflict) {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
		return true
	}
	return false
}

func getCoursesHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courses, err := ts.GetCourses(r.Context())
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(domain.TeacherCoursesResponse{Courses: courses}))
	}
}

func getCourseDetailHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId is required"))
			return
		}

		detail, err := ts.GetCourseDetail(r.Context(), courseID)
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(detail))
	}
}

func getSessionDetailHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		sessionID := chi.URLParam(r, "sessionId")

		if courseID == "" || sessionID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId and sessionId are required"))
			return
		}

		result, err := ts.GetSessionDetail(r.Context(), courseID, sessionID)
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, successResponse(result.Detail))
	}
}

func toggleCheckinHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		sessionID := chi.URLParam(r, "sessionId")

		var req domain.ToggleCheckinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}

		if req.StudentID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("student_id is required"))
			return
		}

		if err := ts.ToggleCheckin(r.Context(), courseID, sessionID, req.StudentID, req.Checked); err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, successResponse(domain.ToggleCheckinResponse{
			StudentID: req.StudentID,
			CheckedIn: req.Checked,
			NewCount:  0,
		}))

	}
}

// listDashboardViewsHandler returns all saved dashboard views.
func listDashboardViewsHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		views, err := svc.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		if views == nil {
			views = []domain.SavedDashboardView{}
		}
		writeJSON(w, http.StatusOK, successResponse(views))
	}
}

// getDashboardViewHandler returns a single saved dashboard view by ID.
func getDashboardViewHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}
		view, err := svc.GetByID(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse("view not found"))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(view))
	}
}

// createDashboardViewHandler creates a new saved dashboard view.
func createDashboardViewHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string                  `json:"name"`
			Filters domain.DashboardFilters `json:"filters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("name is required"))
			return
		}

		view, err := svc.Create(r.Context(), req.Name, req.Filters)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusCreated, successResponse(view))
	}
}

// updateDashboardViewHandler updates an existing saved dashboard view.
func updateDashboardViewHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		var req struct {
			Name    string                  `json:"name"`
			Filters domain.DashboardFilters `json:"filters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("name is required"))
			return
		}

		view, err := svc.Update(r.Context(), id, req.Name, req.Filters)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(view))
	}
}

// deleteDashboardViewHandler deletes a saved dashboard view.
func deleteDashboardViewHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		if err := svc.Delete(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

// touchDashboardViewHandler updates the last_used_at timestamp for a view.
func touchDashboardViewHandler(svc *service.DashboardViewService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		if err := svc.Touch(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func getCourseAttendanceReportHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId is required"))
			return
		}

		threshold := 0
		if t := r.URL.Query().Get("threshold"); t != "" {
			val, err := strconv.Atoi(t)
			if err != nil || val < 0 {
				writeJSON(w, http.StatusBadRequest, errorResponse("threshold must be a non-negative integer"))
				return
			}
			threshold = val
		}

		source := r.URL.Query().Get("source")

		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		report, err := ts.GetAttendanceReport(ctx, courseID, threshold, source)
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, successResponse(report))
	}
}

// getBatchAttendanceHandler returns attendance reports for multiple courses in a single request.
func getBatchAttendanceHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ts == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}

		var req struct {
			CourseIds []string `json:"course_ids"`
			Threshold int      `json:"threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if len(req.CourseIds) == 0 {
			writeJSON(w, http.StatusBadRequest, errorResponse("course_ids is required"))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		batchResult, err := ts.GetBatchAttendance(ctx, req.CourseIds, req.Threshold)
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			if ctx.Err() != nil {
				writeJSON(w, http.StatusGatewayTimeout, errorResponse("batch computation timed out"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		courses := make(map[string]interface{}, len(batchResult.Courses))
		for cid, res := range batchResult.Courses {
			if res.Err != nil {
				courses[cid] = map[string]interface{}{
					"error": res.Err.Error(),
				}
			} else {
				courses[cid] = res.Report
			}
		}

		writeJSON(w, http.StatusOK, successResponse(map[string]interface{}{
			"courses": courses,
		}))
	}
}

func getAbsenceDashboardHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := domain.DefaultDashboardFilters()
		if f := r.URL.Query().Get("filters"); f != "" {
			decoded, err := domain.UnmarshalDashboardFilters([]byte(f))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorResponse("invalid filters parameter"))
				return
			}
			filters = decoded
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		report, err := ts.GetAbsenceDashboard(ctx, filters)
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			if ctx.Err() != nil {
				writeJSON(w, http.StatusGatewayTimeout, errorResponse("dashboard computation timed out"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, successResponse(report))
	}
}
