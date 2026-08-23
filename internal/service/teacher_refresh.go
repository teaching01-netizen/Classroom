package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"qr-command-center/internal/domain"
)

const refreshAllConcurrency = 4

type RefreshAllResult struct {
	CoursesDiscovered  int  `json:"courses_discovered"`
	CoursesRefreshed   int  `json:"courses_refreshed"`
	SessionsDiscovered int  `json:"sessions_discovered"`
	SessionsRefreshed  int  `json:"sessions_refreshed"`
	ProfilesRefreshed  bool `json:"profiles_refreshed"`
	FailedTargets      int  `json:"failed_targets"`
}

func (s *TeacherService) RefreshAllData(ctx context.Context) (RefreshAllResult, error) {
	if !s.snapshotMode {
		return RefreshAllResult{}, nil
	}

	snapshots, ok := s.reader.(snapshotAwareReader)
	if !ok {
		return RefreshAllResult{}, errors.New("teacher: full refresh requires snapshot reader")
	}
	if err := s.refresher.RefreshNow(ctx, snapshots.CatalogRef()); err != nil {
		return RefreshAllResult{}, fmt.Errorf("refresh course catalog: %w", err)
	}

	courses, err := s.reader.GetCourseCatalog(ctx)
	if err != nil {
		return RefreshAllResult{}, fmt.Errorf("read refreshed course catalog: %w", err)
	}

	result := RefreshAllResult{CoursesDiscovered: len(courses)}
	details := make([]*domain.CourseDetail, len(courses))
	var resultMu sync.Mutex
	recordFailure := func() {
		resultMu.Lock()
		result.FailedTargets++
		resultMu.Unlock()
	}

	if err := runBoundedJobs(ctx, len(courses)+1, refreshAllConcurrency, func(index int) {
		if index == 0 {
			if err := s.refresher.RefreshNow(ctx, snapshots.ProfilesRef()); err != nil {
				recordFailure()
				return
			}
			resultMu.Lock()
			result.ProfilesRefreshed = true
			resultMu.Unlock()
			return
		}

		course := courses[index-1]
		courseRef := snapshots.CourseRef(course.CourseID)
		if err := s.refresher.RefreshNow(ctx, courseRef); err != nil {
			recordFailure()
			return
		}
		detail, err := s.reader.GetCourseDetail(ctx, course.CourseID)
		if err != nil || detail == nil {
			recordFailure()
			return
		}
		resultMu.Lock()
		result.CoursesRefreshed++
		details[index-1] = detail
		resultMu.Unlock()
	}); err != nil {
		return result, fmt.Errorf("refresh course data: %w", err)
	}

	sessionRefs := make([]domain.TargetRef, 0)
	seenSessions := make(map[string]struct{})
	for courseIndex, detail := range details {
		if detail == nil {
			continue
		}
		for _, session := range detail.Sessions {
			if session.SessionID == "" {
				continue
			}
			ref := snapshots.SessionRef(courses[courseIndex].CourseID, session.SessionID)
			if _, ok := seenSessions[ref.IdentityKey()]; ok {
				continue
			}
			seenSessions[ref.IdentityKey()] = struct{}{}
			sessionRefs = append(sessionRefs, ref)
		}
	}
	result.SessionsDiscovered = len(sessionRefs)

	if err := runBoundedJobs(ctx, len(sessionRefs), refreshAllConcurrency, func(index int) {
		if err := s.refresher.RefreshNow(ctx, sessionRefs[index]); err != nil {
			recordFailure()
			return
		}
		resultMu.Lock()
		result.SessionsRefreshed++
		resultMu.Unlock()
	}); err != nil {
		return result, fmt.Errorf("refresh session data: %w", err)
	}

	return result, nil
}
