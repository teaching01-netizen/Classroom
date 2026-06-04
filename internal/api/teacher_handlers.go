package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
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

// getAbsenceDashboardHandler returns a cross-course absence dashboard.
// It aggregates attendance data across all (or filtered) courses.
func getAbsenceDashboardHandler(cc *warwick.ClassroomClient, checkinRepo db.SessionCheckinRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cc == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("Warwick client not available"))
			return
		}

		// Parse filters from query param.
		filters := domain.DefaultDashboardFilters()
		if f := r.URL.Query().Get("filters"); f != "" {
			decoded, err := domain.UnmarshalDashboardFilters([]byte(f))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorResponse("invalid filters parameter"))
				return
			}
			filters = decoded
		}

		// Determine threshold: use filter value or default to 20% (0 = auto).
		threshold := filters.Threshold

		// Build data source (DB pre-warmed with Warwick live fallback).
		var dataSource warwick.SessionDataSource
		if checkinRepo != nil {
			dataSource = warwick.NewFallbackSessionDataSource(
				warwick.NewDBSessionDataSource(checkinRepo),
				warwick.NewLiveSessionDataSource(cc),
			)
		} else {
			dataSource = warwick.NewLiveSessionDataSource(cc)
		}

		// Fetch all courses from Warwick.
		allCourses, err := cc.GetCourses()
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

		// Filter to requested course IDs if specified.
		courses := allCourses
		if len(filters.CourseIds) > 0 {
			idSet := make(map[string]bool, len(filters.CourseIds))
			for _, id := range filters.CourseIds {
				idSet[id] = true
			}
			courses = make([]domain.CourseSummary, 0)
			for _, c := range allCourses {
				if idSet[c.CourseID] {
					courses = append(courses, c)
				}
			}
		}

		if len(courses) == 0 {
			writeJSON(w, http.StatusOK, successResponse(domain.DashboardReport{
				GeneratedAt: time.Now(),
				Students:    []domain.StudentAbsence{},
				TopAtRisk:   []domain.StudentRisk{},
				Sessions:    []domain.DashboardSessionSummary{},
			}))
			return
		}

		// Compute attendance reports for each course in parallel.
		type courseResult struct {
			courseID   string
			courseName string
			report     *domain.CourseAttendanceReport
			err        error
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		results := make([]courseResult, len(courses))
		sem := make(chan struct{}, 2) // match teacher tier capacity (default 2 sessions)

		for i, course := range courses {
			sem <- struct{}{}
			go func(idx int, c domain.CourseSummary) {
				defer func() { <-sem }()

				// Retry with backoff on pool exhaustion (other goroutines hold sessions).
				var detail *domain.CourseDetail
				var lastErr error
				for attempt := 0; attempt < 3; attempt++ {
					var err error
					detail, err = cc.GetCourseDetail(c.CourseID)
					if err == nil {
						lastErr = nil
						break
					}
					lastErr = err
					if errors.Is(err, domain.ErrPoolExhausted) {
						backoff := time.Duration(500*(1<<uint(attempt))) * time.Millisecond
						slog.Warn("dashboard_course_detail_pool_retry", "course_id", c.CourseID, "attempt", attempt+1, "backoff", backoff)
						select {
						case <-time.After(backoff):
							continue
						case <-ctx.Done():
							results[idx] = courseResult{courseID: c.CourseID, courseName: c.Name, err: ctx.Err()}
							return
						}
					}
					// Non-pool errors (auth, etc.) — don't retry.
					break
				}
				if lastErr != nil {
					slog.Warn("dashboard_course_detail_failed", "course_id", c.CourseID, "error", lastErr)
					results[idx] = courseResult{courseID: c.CourseID, courseName: c.Name, err: lastErr}
					return
				}

				report := warwick.ComputeCourseAttendanceReport(ctx, dataSource, detail, threshold)
				results[idx] = courseResult{courseID: c.CourseID, courseName: c.Name, report: report}
			}(i, course)
		}

		// Drain semaphore.
		for i := 0; i < cap(sem); i++ {
			sem <- struct{}{}
		}

		if ctx.Err() != nil {
			writeJSON(w, http.StatusGatewayTimeout, errorResponse("dashboard computation timed out"))
			return
		}

		// Aggregate across courses.
		// Track per-student data across courses.
		type studentAgg struct {
			name        string
			nickname    string
			school      string
			avatarURL   string
			attended    int
			total       int
			courses     []domain.CourseAbsence
			perSession  map[string]bool // sessionID → checkedIn
		}

		studentMap := make(map[string]*studentAgg)
		allSessions := make(map[string]*domain.DashboardSessionSummary)
		totalStudents := 0
		totalAtRisk := 0
		totalAttended := 0
		totalSessions := 0

		for _, res := range results {
			if res.err != nil || res.report == nil {
				continue
			}

			courseSessions := make(map[string]bool)
			for _, sess := range res.report.Sessions {
				courseSessions[sess.SessionID] = true
				if _, exists := allSessions[sess.SessionID]; !exists {
				allSessions[sess.SessionID] = &domain.DashboardSessionSummary{
					SessionID:     sess.SessionID,
					SessionNumber: sess.SessionNumber,
					Name:          sess.Name,
					CourseID:      res.courseID,
					CourseName:    res.courseName,
					Status:        string(sess.Status),
				}
				}
			}

			// Count checked-in per session for session summaries.
			for _, s := range res.report.Students {
				for _, ps := range s.PerSession {
					if sess, ok := allSessions[ps.SessionID]; ok {
						if ps.CheckedIn {
							sess.CheckedInCount++
						}
					}
				}
			}

			for _, s := range res.report.Students {
				agg, ok := studentMap[s.StudentID]
				if !ok {
					agg = &studentAgg{
						name:      s.Name,
						nickname:  s.Nickname,
						school:    s.School,
						avatarURL: s.AvatarURL,
						perSession: make(map[string]bool),
					}
					studentMap[s.StudentID] = agg
				}

				agg.attended += s.AttendedSessions
				agg.total += s.TotalSessions
				agg.courses = append(agg.courses, domain.CourseAbsence{
					CourseID:         res.courseID,
					CourseName:       res.courseName,
					TotalSessions:    s.TotalSessions,
					AttendedSessions: s.AttendedSessions,
					Rate:             s.AttendanceRate,
					Absences:         s.TotalSessions - s.AttendedSessions,
					AtRisk:           s.AtRisk,
				})

				for _, ps := range s.PerSession {
					agg.perSession[ps.SessionID] = ps.CheckedIn
				}
			}

			// Count total students from the first report (approximate).
			if totalStudents == 0 {
				totalStudents = len(res.report.Students)
			} else {
				// Approximate: max students across courses
				if len(res.report.Students) > totalStudents {
					totalStudents = len(res.report.Students)
				}
			}
		}

		// Build session list sorted by course then session number.
		sessions := make([]domain.DashboardSessionSummary, 0, len(allSessions))
		for _, s := range allSessions {
			sessions = append(sessions, *s)
		}
		sort.Slice(sessions, func(i, j int) bool {
			if sessions[i].CourseID != sessions[j].CourseID {
				return sessions[i].CourseID < sessions[j].CourseID
			}
			return sessions[i].SessionNumber < sessions[j].SessionNumber
		})

		// Set total students per session.
		for i := range sessions {
			sessions[i].TotalStudents = totalStudents
		}

		// Build student absence list.
		students := make([]domain.StudentAbsence, 0, len(studentMap))
		studentSet := make(map[string]bool)
		for _, agg := range studentMap {
			absences := agg.total - agg.attended
			var rate float64
			if agg.total > 0 {
				rate = float64(agg.attended) / float64(agg.total)
			}

			// Build per-session checkin status for this student.
			perSession := make([]domain.SessionCheckin, 0, len(sessions))
			for _, sess := range sessions {
				checked, hasData := agg.perSession[sess.SessionID]
				status := "not_started"
				if sess.Status == "done" {
					if hasData {
						if checked {
							status = "checked_in"
						} else {
							status = "absent"
						}
					} else {
						status = "no_data"
					}
				} else if sess.Status == "active" {
					if hasData {
						if checked {
							status = "checked_in"
						} else {
							status = "present"
						}
					}
				}

				perSession = append(perSession, domain.SessionCheckin{
					SessionID:     sess.SessionID,
					SessionNumber: sess.SessionNumber,
					SessionName:   sess.Name,
					SessionStatus: sess.Status,
					CheckedIn:     checked,
					Status:        status,
				})
			}

			// Apply threshold: at-risk if absences >= threshold.
			isAtRisk := false
			if threshold > 0 {
				isAtRisk = absences >= threshold
			} else {
				// Default: 20% of total sessions
				defaultThreshold := (agg.total + 4) / 5
				isAtRisk = absences >= defaultThreshold
			}

			if isAtRisk {
				totalAtRisk++
			}
			totalAttended += agg.attended
			totalSessions += agg.total

			// Deduplicate students across courses.
			key := agg.name
			if studentSet[key] {
				continue
			}
			studentSet[key] = true

			students = append(students, domain.StudentAbsence{
				StudentID:        "", // no universal student ID across courses
				Name:             agg.name,
				Nickname:         agg.nickname,
				School:           agg.school,
				AvatarURL:        agg.avatarURL,
				AttendedSessions: agg.attended,
				TotalSessions:    agg.total,
				AttendanceRate:   rate,
				AtRisk:           isAtRisk,
				Courses:          agg.courses,
				PerSession:       perSession,
			})
		}

		// Sort students: at-risk first, then by rate asc, then by name.
		sort.Slice(students, func(i, j int) bool {
			if students[i].AtRisk != students[j].AtRisk {
				return students[i].AtRisk
			}
			if students[i].AttendanceRate != students[j].AttendanceRate {
				return students[i].AttendanceRate < students[j].AttendanceRate
			}
			return students[i].Name < students[j].Name
		})

		// Build top-N at-risk students.
		topAtRisk := make([]domain.StudentRisk, 0)
		for _, s := range students {
			if len(topAtRisk) >= 5 {
				break
			}
			if !s.AtRisk {
				break
			}
			courseName := ""
			if len(s.Courses) > 0 {
				courseName = s.Courses[0].CourseName
			}
			topAtRisk = append(topAtRisk, domain.StudentRisk{
				StudentID:      s.StudentID,
				Name:           s.Name,
				Nickname:       s.Nickname,
				School:         s.School,
				AvatarURL:      s.AvatarURL,
				AttendanceRate: s.AttendanceRate,
				Absences:       s.TotalSessions - s.AttendedSessions,
				TotalSessions:  s.TotalSessions,
				CourseName:     courseName,
			})
		}

		var avgRate float64
		if totalSessions > 0 {
			avgRate = float64(totalAttended) / float64(totalSessions)
		}

		report := domain.DashboardReport{
			GeneratedAt:       time.Now(),
			TotalStudents:     totalStudents,
			TotalCourses:      len(courses),
			AvgAttendanceRate: avgRate,
			AtRiskCount:       totalAtRisk,
			TopAtRisk:         topAtRisk,
			Students:          students,
			Sessions:          sessions,
		}

		writeJSON(w, http.StatusOK, successResponse(report))
	}
}
