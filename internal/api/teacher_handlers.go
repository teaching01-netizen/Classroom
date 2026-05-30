package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
			if err == domain.ErrAuthExpired {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
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
			if err == domain.ErrAuthExpired {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
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
			if err == domain.ErrAuthExpired {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
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
			if err == domain.ErrAuthExpired {
				writeJSON(w, http.StatusUnauthorized, errorResponse("Warwick session expired"))
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
