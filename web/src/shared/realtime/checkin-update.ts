// Minimal structural shape of a session-detail cache entry. Kept local so
// this module stays feature-free (unlike room-events.ts, which is allowed to
// import the rooms feature per the architecture carve-out).
export type CheckinDetailShape = {
  readonly students: readonly { student_id: string; checked_in: boolean }[]
  readonly checked_in_count?: number
}

// isSessionDetailLike narrows a cached query entry to a session detail before
// an in-place check-in delta is applied. The ['sessions'] query prefix covers
// both course details (no students array) and session details.
export function isSessionDetailLike(value: unknown): value is CheckinDetailShape {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  return Array.isArray((value as CheckinDetailShape).students)
}

export function applyCheckinDelta<T extends CheckinDetailShape>(
  detail: T,
  studentId: string,
  checkedIn: boolean,
): T {
  const students = detail.students.map((student) =>
    student.student_id === studentId ? { ...student, checked_in: checkedIn } : student,
  )
  return {
    ...detail,
    students,
    checked_in_count: students.filter((student) => student.checked_in).length,
  }
}
