package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/warwick"
)

func getCoursesHandler(cc *warwick.ClassroomClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}
		courses, err := cc.GetCourses()
		if err != nil {
			if errors.Is(err, domain.ErrAuthExpired) {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
				return
			}
			if errors.Is(err, domain.ErrPoolExhausted) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
				return
			}
			if errors.Is(err, domain.ErrAuthConflict) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(domain.TeacherCoursesResponse{Courses: courses}))
	}
}

func getCourseDetailHandler(cc *warwick.ClassroomClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}
		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId is required"))
			return
		}

		detail, err := cc.GetCourseDetail(courseID)
		if err != nil {
			if errors.Is(err, domain.ErrAuthExpired) {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
				return
			}
			if errors.Is(err, domain.ErrPoolExhausted) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
				return
			}
			if errors.Is(err, domain.ErrAuthConflict) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(detail))
	}
}

func getSessionDetailHandler(cc *warwick.ClassroomClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}
		courseID := chi.URLParam(r, "courseId")
		sessionID := chi.URLParam(r, "sessionId")

		if courseID == "" || sessionID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId and sessionId are required"))
			return
		}

		detail, err := cc.GetSessionDetail(courseID, sessionID)
		if err != nil {
			if errors.Is(err, domain.ErrAuthExpired) {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
				return
			}
			if errors.Is(err, domain.ErrPoolExhausted) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
				return
			}
			if errors.Is(err, domain.ErrAuthConflict) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(detail))
	}
}

func toggleCheckinHandler(cc *warwick.ClassroomClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}
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

		if err := cc.ToggleCheckin(courseID, sessionID, req.StudentID, req.Checked); err != nil {
			if errors.Is(err, domain.ErrAuthExpired) {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
				return
			}
			if errors.Is(err, domain.ErrPoolExhausted) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
				return
			}
			if errors.Is(err, domain.ErrAuthConflict) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
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

		// Mark the attendance report stale (not hard invalidate).
		// Stale-while-revalidate: next request returns stale + triggers async refresh.
		cc.MarkStaleReport(courseID)
	}
}

// listDashboardViewsHandler returns all saved dashboard views.
func listDashboardViewsHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		views, err := viewRepo.List(r.Context())
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
func getDashboardViewHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}
		view, err := viewRepo.GetByID(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse("view not found"))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(view))
	}
}

// createDashboardViewHandler creates a new saved dashboard view.
func createDashboardViewHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string                `json:"name"`
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

		view, err := viewRepo.Create(r.Context(), req.Name, req.Filters)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusCreated, successResponse(view))
	}
}

// updateDashboardViewHandler updates an existing saved dashboard view.
func updateDashboardViewHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		var req struct {
			Name    string                `json:"name"`
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

		view, err := viewRepo.Update(r.Context(), id, req.Name, req.Filters)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(view))
	}
}

// deleteDashboardViewHandler deletes a saved dashboard view.
func deleteDashboardViewHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		if err := viewRepo.Delete(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

// touchDashboardViewHandler updates the last_used_at timestamp for a view.
func touchDashboardViewHandler(viewRepo db.DashboardViewRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid view id"))
			return
		}

		if err := viewRepo.Touch(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func getCourseAttendanceReportHandler(cc *warwick.ClassroomClient, checkinRepo db.SessionCheckinRepository, persister warwick.ReportEnqueuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}
		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("courseId is required"))
			return
		}

		// Parse threshold query param (number of absences allowed).
		// 0 or empty = default (20% of total sessions).
		threshold := 0
		if t := r.URL.Query().Get("threshold"); t != "" {
			val, err := strconv.Atoi(t)
			if err != nil || val < 0 {
				writeJSON(w, http.StatusBadRequest, errorResponse("threshold must be a non-negative integer"))
				return
			}
			threshold = val
		}

		// Parse source query param. Default = "db" (pre-warmed data).
		// "live" = fetch directly from Warwick API (slower, rate-limited).
		// DB source uses fallback: if DB has 0 students for a session
		// (prewarmer hasn't synced yet), it retries from Warwick live.
		source := r.URL.Query().Get("source")
		var dataSource warwick.SessionDataSource
		if source == "live" {
			dataSource = warwick.NewLiveSessionDataSource(cc)
		} else {
			if checkinRepo == nil {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("DB source not available; use ?source=live"))
				return
			}
			dataSource = warwick.NewFallbackSessionDataSource(
				warwick.NewDBSessionDataSource(checkinRepo),
				warwick.NewLiveSessionDataSource(cc),
			)
		}

		// Fetch course detail for the session list.
		courseDetail, err := cc.GetCourseDetail(courseID)
		if err != nil {
			if errors.Is(err, domain.ErrAuthExpired) {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
				return
			}
			if errors.Is(err, domain.ErrPoolExhausted) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Too many concurrent requests, try again"))
				return
			}
			if errors.Is(err, domain.ErrAuthConflict) {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick session in use, try again"))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		// Compute report with a generous timeout (live fetch of all sessions).
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		report, err := cc.GetCourseAttendanceReport(ctx, courseID, courseDetail.Name, courseDetail.Sessions, threshold, dataSource, persister)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, successResponse(report))
	}
}
