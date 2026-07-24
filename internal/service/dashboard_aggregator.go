package service

import (
	"sort"
	"time"

	"qr-command-center/internal/domain"
)

type studentAgg struct {
	studentGUID string
	name        string
	nickname    string
	school      string
	avatarURL   string
	attended    int
	total       int
	courses     []domain.CourseAbsence
	perSession  map[string]bool
}

func aggregatePerCourseResults(results []dashboardCourseResult) (map[string]*studentAgg, map[string]*domain.DashboardSessionSummary, int) {
	studentMap := make(map[string]*studentAgg)
	allSessions := make(map[string]*domain.DashboardSessionSummary)

	for _, res := range results {
		if res.err != nil || res.report == nil {
			continue
		}

		for _, sess := range res.report.Sessions {
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
					studentGUID: s.StudentID,
					name:        s.Name,
					nickname:    s.Nickname,
					school:      s.School,
					avatarURL:   s.AvatarURL,
					perSession:  make(map[string]bool),
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

	}

	return studentMap, allSessions, len(studentMap)
}

func buildSessionList(allSessions map[string]*domain.DashboardSessionSummary, totalStudents int) []domain.DashboardSessionSummary {
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
	for i := range sessions {
		sessions[i].TotalStudents = totalStudents
	}
	return sessions
}

func buildStudentIDMapping(profiles []domain.StudentProfile) map[string]string {
	m := make(map[string]string, len(profiles))
	for _, p := range profiles {
		if p.StudentID != "" && p.StudentGuid != "" {
			m[p.StudentGuid] = p.StudentID
		}
	}
	return m
}

func buildStudentAbsences(studentMap map[string]*studentAgg, sessions []domain.DashboardSessionSummary, guidToStudentID map[string]string, threshold int) []domain.StudentAbsence {
	students := make([]domain.StudentAbsence, 0, len(studentMap))
	studentSet := make(map[string]bool)

	for _, agg := range studentMap {
		absences := agg.total - agg.attended
		var rate float64
		if agg.total > 0 {
			rate = float64(agg.attended) / float64(agg.total)
		}

		perSession := make([]domain.SessionCheckin, 0, len(sessions))
		for _, sess := range sessions {
			checked, hasData := agg.perSession[sess.SessionID]
			status := computeSessionStatus(sess.Status, hasData, checked)
			perSession = append(perSession, domain.SessionCheckin{
				SessionID:     sess.SessionID,
				SessionNumber: sess.SessionNumber,
				SessionName:   sess.Name,
				SessionDate:   sess.Date,
				SessionStatus: sess.Status,
				CheckedIn:     checked,
				Status:        status,
			})
		}

		isAtRisk := computeAtRisk(agg.total, absences, threshold)

		// Names are not stable identifiers: siblings or classmates can share a
		// display name. Only fall back to the name when upstream omitted an ID.
		key := agg.studentGUID
		if key == "" {
			key = agg.name
		}
		if studentSet[key] {
			continue
		}
		studentSet[key] = true

		students = append(students, domain.StudentAbsence{
			StudentID:        guidToStudentID[agg.studentGUID],
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

	sort.Slice(students, func(i, j int) bool {
		if students[i].AtRisk != students[j].AtRisk {
			return students[i].AtRisk
		}
		if students[i].AttendanceRate != students[j].AttendanceRate {
			return students[i].AttendanceRate < students[j].AttendanceRate
		}
		return students[i].Name < students[j].Name
	})

	return students
}

func computeSessionStatus(sessionStatus string, hasData bool, checked bool) string {
	if sessionStatus == "done" {
		if hasData {
			if checked {
				return "checked_in"
			}
			return "absent"
		}
	} else if sessionStatus == "active" {
		if hasData {
			if checked {
				return "checked_in"
			}
			return "present"
		}
	}
	return "not_started"
}

func computeAtRisk(totalSessions, absences, threshold int) bool {
	if threshold > 0 {
		return absences >= threshold
	}
	defaultThreshold := (totalSessions + 4) / 5
	return absences >= defaultThreshold
}

func extractTopAtRisk(students []domain.StudentAbsence, limit int) []domain.StudentRisk {
	top := make([]domain.StudentRisk, 0, limit)
	for _, st := range students {
		if len(top) >= limit {
			break
		}
		if !st.AtRisk {
			break
		}
		courseName := ""
		if len(st.Courses) > 0 {
			courseName = st.Courses[0].CourseName
		}
		top = append(top, domain.StudentRisk{
			StudentID:      st.StudentID,
			Name:           st.Name,
			Nickname:       st.Nickname,
			School:         st.School,
			AvatarURL:      st.AvatarURL,
			AttendanceRate: st.AttendanceRate,
			Absences:       st.TotalSessions - st.AttendedSessions,
			TotalSessions:  st.TotalSessions,
			CourseName:     courseName,
		})
	}
	return top
}

func (s *TeacherService) aggregateDashboard(
	results []dashboardCourseResult,
	courses []domain.CourseSummary,
	threshold int,
	wCodes []string,
	guidToStudentID map[string]string,
) (*domain.DashboardReport, error) {

	studentMap, allSessions, totalStudents := aggregatePerCourseResults(results)
	sessions := buildSessionList(allSessions, totalStudents)
	students := buildStudentAbsences(studentMap, sessions, guidToStudentID, threshold)

	if len(wCodes) > 0 {
		students = domain.FilterStudentsByWCodes(students, wCodes)
	}

	atRiskCount := 0
	attendedSessions := 0
	totalSessions := 0
	for _, student := range students {
		if student.AtRisk {
			atRiskCount++
		}
		attendedSessions += student.AttendedSessions
		totalSessions += student.TotalSessions
	}
	avgAttendanceRate := 0.0
	if totalSessions > 0 {
		avgAttendanceRate = float64(attendedSessions) / float64(totalSessions)
	}
	topAtRisk := extractTopAtRisk(students, 5)

	return &domain.DashboardReport{
		GeneratedAt:       time.Now(),
		TotalStudents:     totalStudents,
		TotalCourses:      len(courses),
		AvgAttendanceRate: avgAttendanceRate,
		AtRiskCount:       atRiskCount,
		TopAtRisk:         topAtRisk,
		Students:          students,
		Sessions:          sessions,
	}, nil
}
