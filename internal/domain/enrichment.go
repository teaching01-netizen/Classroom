package domain

// buildGUIDToWCodeMap creates a lookup from StudentGuid (UUID) to StudentID (wcode).
func buildGUIDToWCodeMap(profiles []StudentProfile) map[string]string {
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
func EnrichStudentIDWithWCode(students []StudentAttendance, profiles []StudentProfile) []StudentAttendance {
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
func EnrichCheckinStudentIDWithWCode(students []StudentCheckin, profiles []StudentProfile) []StudentCheckin {
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

// ResolveCheckinStudentID returns the Humanix roster identifier for a public
// student identifier. Public APIs expose the WCode from StudentProfile, while
// attendance mutations require the corresponding StudentGuid. A profile is
// accepted only when its GUID is present in the requested session roster.
func ResolveCheckinStudentID(requestedID string, students []StudentCheckin, profiles []StudentProfile) (string, bool) {
	if requestedID == "" {
		return "", false
	}

	rosterIDs := make(map[string]struct{}, len(students))
	for _, student := range students {
		if student.StudentID == "" {
			continue
		}
		rosterIDs[student.StudentID] = struct{}{}
	}
	if _, ok := rosterIDs[requestedID]; ok {
		return requestedID, true
	}

	for _, profile := range profiles {
		if profile.StudentID != requestedID || profile.StudentGuid == "" {
			continue
		}
		if _, ok := rosterIDs[profile.StudentGuid]; ok {
			return profile.StudentGuid, true
		}
	}
	return "", false
}
