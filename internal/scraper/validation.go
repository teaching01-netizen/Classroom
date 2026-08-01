package scraper

import (
	"fmt"
	"strings"
	"time"

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
	MaxMissingIDRatio    float64
	ConfirmationAttempts int
}

func DefaultChangeSafetyPolicy() ChangeSafetyPolicy {
	return ChangeSafetyPolicy{
		MinimumPreviousCount: 2,
		MaximumDropRatio:     0.20,
		MaxMissingIDRatio:    0.10,
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

// resourceKey is the target's ResourceKey (the stable upstream identity the
// fetch was requested for); kind-specific validators use it to reject a
// response that was served for a different resource.
func ValidatePayload(kind domain.SnapshotKind, raw any, resourceKey string) (*ValidatedPayload, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil payload")
	}

	switch kind {
	case domain.SnapshotCourseCatalog:
		return validateCourseCatalog(raw)
	case domain.SnapshotCourseDetail:
		return validateCourseDetail(raw, resourceKey)
	case domain.SnapshotSessionDetail:
		return validateSessionDetail(raw, resourceKey)
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

func validateCourseDetail(raw any, resourceKey string) (*ValidatedPayload, error) {
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

	if resourceKey != "" && detail.CourseID != resourceKey {
		return nil, fmt.Errorf(
			"course_detail: CourseID %q does not match requested resource key %q",
			detail.CourseID,
			resourceKey,
		)
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

		if status := strings.TrimSpace(string(session.Status)); status != "" {
			if _, err := domain.GetSessionStatus(status); err != nil {
				return nil, fmt.Errorf(
					"course_detail: session %s has invalid SessionStatus %q",
					session.SessionID,
					session.Status,
				)
			}
		}

		if session.Date != "" {
			if _, err := time.Parse("2006-01-02", session.Date); err != nil {
				return nil, fmt.Errorf(
					"course_detail: session %s has invalid date %q",
					session.SessionID,
					session.Date,
				)
			}
		}
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

func validateSessionDetail(raw any, resourceKey string) (*ValidatedPayload, error) {
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

	if resourceKey != "" && detail.SessionID != resourceKey {
		return nil, fmt.Errorf(
			"session_detail: SessionID %q does not match requested resource key %q",
			detail.SessionID,
			resourceKey,
		)
	}

	var warnings []ValidationWarning
	seen := make(map[string]bool, len(detail.Students))
	checkedIn := 0

	for _, student := range detail.Students {
		if strings.TrimSpace(student.StudentID) == "" {
			return nil, fmt.Errorf("session_detail: student has empty StudentID")
		}
		if seen[student.StudentID] {
			return nil, fmt.Errorf("session_detail: duplicate StudentID %q", student.StudentID)
		}
		seen[student.StudentID] = true
		if student.CheckedIn {
			checkedIn++
		}
	}

	if checkedIn != detail.CheckedInCount {
		return nil, fmt.Errorf(
			"session_detail: CheckedInCount %d does not match %d checked-in students",
			detail.CheckedInCount,
			checkedIn,
		)
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

		if strings.TrimSpace(profile.StudentGuid) == "" {
			warnings = append(warnings, ValidationWarning{
				Code:    "missing_student_guid",
				Message: fmt.Sprintf("student %s has empty StudentGuid", profile.StudentID),
			})
		}
		if strings.TrimSpace(profile.FullName) == "" {
			warnings = append(warnings, ValidationWarning{
				Code:    "missing_full_name",
				Message: fmt.Sprintf("student %s has empty FullName", profile.StudentID),
			})
		}
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
	return ClassifyChangeAgainst(policy, validated, previousCount, nil)
}

// ClassifyChangeAgainst extends ClassifyChange with an ID-based suspicion
// rule: when the previous snapshot's stable IDs are known and most of them
// are absent from the candidate, the change is treated as suspicious even if
// the drop ratio stays within tolerance. previousIDs == nil disables the
// rule, so ClassifyChange is exactly ClassifyChangeAgainst(..., nil).
func ClassifyChangeAgainst(
	policy ChangeSafetyPolicy,
	validated *ValidatedPayload,
	previousCount int,
	previousIDs map[string]struct{},
) ClassifiedChange {
	result := ClassifiedChange{
		Status:        "changed",
		PreviousCount: previousCount,
		NewCount:      validated.RecordCount,
		Warnings:      validated.Warnings,
	}

	if previousCount <= 0 {
		return result
	}

	if previousCount > 0 {
		result.DropRatio = float64(previousCount-validated.RecordCount) / float64(previousCount)
	}

	// A previously non-empty dataset becoming empty is never published
	// without an independent confirmation fetch.
	if validated.RecordCount == 0 {
		result.Status = "suspicious"
		metrics.ScrapeSuspiciousResponseTotal.
			WithLabelValues(string(validated.Kind), "became_empty").Inc()
		return result
	}

	if previousCount >= policy.MinimumPreviousCount &&
		result.DropRatio > policy.MaximumDropRatio {
		result.Status = "suspicious"
		metrics.ScrapeSuspiciousResponseTotal.
			WithLabelValues(string(validated.Kind), "large_drop").Inc()
		return result
	}

	if previousCount >= policy.MinimumPreviousCount && len(previousIDs) > 0 {
		currentIDs := validatedIDs(validated)
		if currentIDs == nil {
			// The candidate's ID set cannot be decoded (unrecognized shape
			// or missing raw value): treat every previous ID as missing
			// would be a false permanent suspicion, so skip the rule.
			return result
		}
		missing := 0
		for id := range previousIDs {
			if _, present := currentIDs[id]; !present {
				missing++
			}
		}
		missingRatio := float64(missing) / float64(len(previousIDs))
		if missingRatio > policy.MaxMissingIDRatio {
			result.Status = "suspicious"
			metrics.ScrapeSuspiciousResponseTotal.
				WithLabelValues(string(validated.Kind), "many_missing_ids").Inc()
			return result
		}
	}

	return result
}

// validatedIDs extracts the stable ID set from a validated payload's Raw
// value, mirroring the per-kind identities used by the validators. It returns
// nil when the kind or value shape is not recognized.
func validatedIDs(validated *ValidatedPayload) map[string]struct{} {
	if validated == nil || validated.Raw == nil {
		return nil
	}
	ids := make(map[string]struct{})
	switch validated.Kind {
	case domain.SnapshotCourseCatalog:
		courses, ok := validated.Raw.([]domain.CourseSummary)
		if !ok {
			return nil
		}
		for _, course := range courses {
			if course.CourseID != "" {
				ids[course.CourseID] = struct{}{}
			}
		}
	case domain.SnapshotCourseDetail:
		var detail domain.CourseDetail
		switch typed := validated.Raw.(type) {
		case domain.CourseDetail:
			detail = typed
		case *domain.CourseDetail:
			if typed == nil {
				return nil
			}
			detail = *typed
		default:
			return nil
		}
		for _, session := range detail.Sessions {
			if session.SessionID != "" {
				ids[session.SessionID] = struct{}{}
			}
		}
	case domain.SnapshotSessionDetail:
		var detail domain.SessionDetail
		switch typed := validated.Raw.(type) {
		case domain.SessionDetail:
			detail = typed
		case *domain.SessionDetail:
			if typed == nil {
				return nil
			}
			detail = *typed
		default:
			return nil
		}
		for _, student := range detail.Students {
			if student.StudentID != "" {
				ids[student.StudentID] = struct{}{}
			}
		}
	case domain.SnapshotStudentProfiles:
		profiles, ok := validated.Raw.([]domain.StudentProfile)
		if !ok {
			return nil
		}
		for _, profile := range profiles {
			if profile.StudentID != "" {
				ids[profile.StudentID] = struct{}{}
			}
		}
	default:
		return nil
	}
	return ids
}
