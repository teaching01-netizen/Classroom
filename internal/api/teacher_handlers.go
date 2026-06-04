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
		source := r.URL.Query().Get("source")
		var dataSource warwick.SessionDataSource
		if source == "live" {
			dataSource = warwick.NewLiveSessionDataSource(cc)
		} else {
			if checkinRepo == nil {
				writeJSON(w, http.StatusServiceUnavailable, errorResponse("DB source not available; use ?source=live"))
				return
			}
			dataSource = warwick.NewDBSessionDataSource(checkinRepo)
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
