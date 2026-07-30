package scraper

import (
	"fmt"
	"strings"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
)

type ValidationWarning struct {
	Code    string
	Message string
}

type ValidatedPayload struct {
	Kind              domain.SnapshotKind
	Raw               any
	RecordCount       int
	DistinctIDs       int
	ValidationVersion int
	Warnings          []ValidationWarning
}

type ChangeSafetyPolicy struct {
	MinimumPreviousCount int
	MaximumDropRatio     float64
	ConfirmationAttempts int
}

func DefaultChangeSafetyPolicy() ChangeSafetyPolicy {
	return ChangeSafetyPolicy{
		MinimumPreviousCount: 20,
		MaximumDropRatio:     0.8,
		ConfirmationAttempts: 2,
	}
}

type ClassifiedChange struct {
	Status        string
	PreviousCount int
	NewCount      int
	DropRatio     float64
	Warnings      []ValidationWarning
}

const validationVersion = 1

func ValidatePayload(kind domain.SnapshotKind, raw any) (*ValidatedPayload, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil payload")
	}

	switch kind {
	case domain.SnapshotCourseCatalog:
		return validateCourseCatalog(raw)
	case domain.SnapshotCourseDetail:
		return validateCourseDetail(raw)
	case domain.SnapshotSessionDetail:
		return validateSessionDetail(raw)
	case domain.SnapshotStudentProfiles:
		return validateStudentProfiles(raw)
	default:
		return nil, fmt.Errorf("unsupported snapshot kind %q", kind)
	}
}

func validateCourseCatalog(raw any) (*ValidatedPayload, error) {
	courses, ok := raw.([]domain.CourseSummary)
	if !ok {
		return nil, fmt.Errorf("course_catalog: expected []domain.CourseSummary, got %T", raw)
	}

	var warnings []ValidationWarning
	seen := make(map[string]bool, len(courses))

	for _, course := range courses {
		if strings.TrimSpace(course.CourseID) == "" {
			return nil, fmt.Errorf("course_catalog: course has empty CourseID")
		}
		if seen[course.CourseID] {
			return nil, fmt.Errorf("course_catalog: duplicate CourseID %q", course.CourseID)
		}
		seen[course.CourseID] = true

		if strings.TrimSpace(course.Name) == "" {
			warnings = append(warnings, ValidationWarning{
				Code:    "missing_name",
				Message: fmt.Sprintf("course %s has empty Name", course.CourseID),
			})
		}
		if strings.TrimSpace(string(course.Status)) == "" {
			warnings = append(warnings, ValidationWarning{
				Code:    "missing_status",
				Message: fmt.Sprintf("course %s has empty Status", course.CourseID),
			})
		}
	}

	return &ValidatedPayload{
		Kind:              domain.SnapshotCourseCatalog,
		Raw:               courses,
		RecordCount:       len(courses),
		DistinctIDs:       len(courses),
		ValidationVersion: validationVersion,
		Warnings:          warnings,
	}, nil
}

func validateCourseDetail(raw any) (*ValidatedPayload, error) {
	var detail domain.CourseDetail
	switch typed := raw.(type) {
	case domain.CourseDetail:
		detail = typed
	case *domain.CourseDetail:
		if typed == nil {
			return nil, fmt.Errorf("course_detail: nil pointer")
		}
		detail = *typed
	default:
		return nil, fmt.Errorf("course_detail: expected domain.CourseDetail, got %T", raw)
	}

	var warnings []ValidationWarning
	seen := make(map[string]bool, len(detail.Sessions))

	for _, session := range detail.Sessions {
		if strings.TrimSpace(session.SessionID) == "" {
			return nil, fmt.Errorf("course_detail: session has empty SessionID")
		}
		if seen[session.SessionID] {
			return nil, fmt.Errorf("course_detail: duplicate SessionID %q", session.SessionID)
		}
		seen[session.SessionID] = true
	}

	return &ValidatedPayload{
		Kind:              domain.SnapshotCourseDetail,
		Raw:               detail,
		RecordCount:       len(detail.Sessions),
		DistinctIDs:       len(detail.Sessions),
		ValidationVersion: validationVersion,
		Warnings:          warnings,
	}, nil
}

func validateSessionDetail(raw any) (*ValidatedPayload, error) {
	var detail domain.SessionDetail
	switch typed := raw.(type) {
	case domain.SessionDetail:
		detail = typed
	case *domain.SessionDetail:
		if typed == nil {
			return nil, fmt.Errorf("session_detail: nil pointer")
		}
		detail = *typed
	default:
		return nil, fmt.Errorf("session_detail: expected domain.SessionDetail, got %T", raw)
	}

	var warnings []ValidationWarning
	seen := make(map[string]bool, len(detail.Students))

	for _, student := range detail.Students {
		if strings.TrimSpace(student.StudentID) == "" {
			return nil, fmt.Errorf("session_detail: student has empty StudentID")
		}
		if seen[student.StudentID] {
			return nil, fmt.Errorf("session_detail: duplicate StudentID %q", student.StudentID)
		}
		seen[student.StudentID] = true
	}

	return &ValidatedPayload{
		Kind:              domain.SnapshotSessionDetail,
		Raw:               detail,
		RecordCount:       len(detail.Students),
		DistinctIDs:       len(detail.Students),
		ValidationVersion: validationVersion,
		Warnings:          warnings,
	}, nil
}

func validateStudentProfiles(raw any) (*ValidatedPayload, error) {
	profiles, ok := raw.([]domain.StudentProfile)
	if !ok {
		return nil, fmt.Errorf("student_profiles: expected []domain.StudentProfile, got %T", raw)
	}

	var warnings []ValidationWarning
	seen := make(map[string]bool, len(profiles))

	for _, profile := range profiles {
		if strings.TrimSpace(profile.StudentID) == "" {
			return nil, fmt.Errorf("student_profiles: profile has empty StudentID")
		}
		if seen[profile.StudentID] {
			return nil, fmt.Errorf("student_profiles: duplicate StudentID %q", profile.StudentID)
		}
		seen[profile.StudentID] = true
	}

	return &ValidatedPayload{
		Kind:              domain.SnapshotStudentProfiles,
		Raw:               profiles,
		RecordCount:       len(profiles),
		DistinctIDs:       len(profiles),
		ValidationVersion: validationVersion,
		Warnings:          warnings,
	}, nil
}

func ClassifyChange(
	policy ChangeSafetyPolicy,
	validated *ValidatedPayload,
	previousCount int,
) ClassifiedChange {
	result := ClassifiedChange{
		Status:        "changed",
		PreviousCount: previousCount,
		NewCount:      validated.RecordCount,
		Warnings:      validated.Warnings,
	}

	if previousCount == 0 {
		result.Status = "changed"
		return result
	}

	if previousCount < policy.MinimumPreviousCount {
		result.Status = "changed"
		return result
	}

	if previousCount > 0 {
		result.DropRatio = float64(previousCount-validated.RecordCount) / float64(previousCount)
	}

	if result.DropRatio > policy.MaximumDropRatio {
		result.Status = "suspicious"
		metrics.ScrapeSuspiciousResponseTotal.
			WithLabelValues(string(validated.Kind), "large_drop").Inc()
		return result
	}

	result.Status = "changed"
	return result
}
