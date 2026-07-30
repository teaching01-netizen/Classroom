package scraper

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"qr-command-center/internal/domain"
)

func Canonicalize(
	kind domain.SnapshotKind,
	value any,
	maxBytes int64,
) (json.RawMessage, [32]byte, int, error) {
	var zero [32]byte
	if maxBytes <= 0 {
		return nil, zero, 0, fmt.Errorf("canonical payload limit must be positive")
	}

	normalized, err := canonicalValue(kind, value)
	if err != nil {
		return nil, zero, 0, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, zero, 0, fmt.Errorf("marshal canonical %s: %w", kind, err)
	}
	if int64(len(payload)) > maxBytes {
		return nil, zero, 0, fmt.Errorf(
			"canonical %s payload exceeds limit: %d > %d bytes",
			kind,
			len(payload),
			maxBytes,
		)
	}
	hash := sha256.Sum256(payload)
	return json.RawMessage(payload), hash, countRecords(normalized), nil
}

func countRecords(normalized any) int {
	switch v := normalized.(type) {
	case []domain.CourseSummary:
		return len(v)
	case []domain.StudentProfile:
		return len(v)
	default:
		return 0
	}
}

func canonicalValue(kind domain.SnapshotKind, value any) (any, error) {
	switch kind {
	case domain.SnapshotCourseCatalog:
		courses, ok := value.([]domain.CourseSummary)
		if !ok {
			return nil, fmt.Errorf("canonical %s expects []domain.CourseSummary, got %T", kind, value)
		}
		copied := append([]domain.CourseSummary(nil), courses...)
		if copied == nil {
			copied = []domain.CourseSummary{}
		}
		sort.SliceStable(copied, func(i, j int) bool {
			if copied[i].CourseID == copied[j].CourseID {
				return copied[i].Name < copied[j].Name
			}
			return copied[i].CourseID < copied[j].CourseID
		})
		return copied, nil
	case domain.SnapshotCourseDetail:
		var detail domain.CourseDetail
		switch typed := value.(type) {
		case domain.CourseDetail:
			detail = typed
		case *domain.CourseDetail:
			if typed == nil {
				return nil, fmt.Errorf("canonical %s received typed nil", kind)
			}
			detail = *typed
		default:
			return nil, fmt.Errorf("canonical %s expects domain.CourseDetail, got %T", kind, value)
		}
		detail.Sessions = append([]domain.SessionSummary(nil), detail.Sessions...)
		if detail.Sessions == nil {
			detail.Sessions = []domain.SessionSummary{}
		}
		sort.SliceStable(detail.Sessions, func(i, j int) bool {
			if detail.Sessions[i].SessionNumber == detail.Sessions[j].SessionNumber {
				return detail.Sessions[i].SessionID < detail.Sessions[j].SessionID
			}
			return detail.Sessions[i].SessionNumber < detail.Sessions[j].SessionNumber
		})
		return detail, nil
	case domain.SnapshotSessionDetail:
		var detail domain.SessionDetail
		switch typed := value.(type) {
		case domain.SessionDetail:
			detail = typed
		case *domain.SessionDetail:
			if typed == nil {
				return nil, fmt.Errorf("canonical %s received typed nil", kind)
			}
			detail = *typed
		default:
			return nil, fmt.Errorf("canonical %s expects domain.SessionDetail, got %T", kind, value)
		}
		detail.Students = append([]domain.StudentCheckin(nil), detail.Students...)
		if detail.Students == nil {
			detail.Students = []domain.StudentCheckin{}
		}
		for index := range detail.Students {
			if detail.Students[index].CheckedInAt != nil {
				value := normalizeTimestampString(*detail.Students[index].CheckedInAt)
				detail.Students[index].CheckedInAt = &value
			}
		}
		if detail.QRExpiresAt != nil {
			value := normalizeTimestampString(*detail.QRExpiresAt)
			detail.QRExpiresAt = &value
		}
		sort.SliceStable(detail.Students, func(i, j int) bool {
			if detail.Students[i].StudentID == detail.Students[j].StudentID {
				return detail.Students[i].Name < detail.Students[j].Name
			}
			return detail.Students[i].StudentID < detail.Students[j].StudentID
		})
		return detail, nil
	case domain.SnapshotStudentProfiles:
		profiles, ok := value.([]domain.StudentProfile)
		if !ok {
			return nil, fmt.Errorf("canonical %s expects []domain.StudentProfile, got %T", kind, value)
		}
		copied := append([]domain.StudentProfile(nil), profiles...)
		if copied == nil {
			copied = []domain.StudentProfile{}
		}
		sort.SliceStable(copied, func(i, j int) bool {
			if copied[i].StudentGuid == copied[j].StudentGuid {
				return copied[i].StudentID < copied[j].StudentID
			}
			return copied[i].StudentGuid < copied[j].StudentGuid
		})
		return copied, nil
	default:
		return nil, fmt.Errorf("canonicalize unknown snapshot kind %q", kind)
	}
}

func normalizeTimestampString(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}
