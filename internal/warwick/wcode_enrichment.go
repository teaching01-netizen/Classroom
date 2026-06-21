package warwick

import "qr-command-center/internal/domain"

// buildGUIDToWCodeMap creates a lookup from StudentGuid (UUID) to StudentID (wcode).
func buildGUIDToWCodeMap(profiles []domain.StudentProfile) map[string]string {
	m := make(map[string]string, len(profiles))
	for _, p := range profiles {
		if p.StudentID != "" && p.StudentGuid != "" {
			m[p.StudentGuid] = p.StudentID
		}
	}
	return m
}

// EnrichStudentIDWithWCode replaces UUID-based StudentIDs in StudentAttendance
// records with the Warwick StudentID (wcode) from student profiles.
// Students whose UUID is not found in profiles keep their original ID.
func EnrichStudentIDWithWCode(students []domain.StudentAttendance, profiles []domain.StudentProfile) []domain.StudentAttendance {
	guidMap := buildGUIDToWCodeMap(profiles)
	if len(guidMap) == 0 {
		return students
	}
	for i := range students {
		if wcode, ok := guidMap[students[i].StudentID]; ok {
			students[i].StudentID = wcode
		}
	}
	return students
}

// EnrichCheckinStudentIDWithWCode replaces UUID-based StudentIDs in StudentCheckin
// records with the Warwick StudentID (wcode) from student profiles.
func EnrichCheckinStudentIDWithWCode(students []domain.StudentCheckin, profiles []domain.StudentProfile) []domain.StudentCheckin {
	guidMap := buildGUIDToWCodeMap(profiles)
	if len(guidMap) == 0 {
		return students
	}
	for i := range students {
		if wcode, ok := guidMap[students[i].StudentID]; ok {
			students[i].StudentID = wcode
		}
	}
	return students
}
