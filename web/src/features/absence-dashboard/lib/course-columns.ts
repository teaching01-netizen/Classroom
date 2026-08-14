import type { AbsenceDashboard } from '../api/absence.schemas'

export type CourseColumn = {
  readonly courseId: string
  readonly courseName: string
}

export function buildCourseColumns(report: AbsenceDashboard): CourseColumn[] {
  const columns: CourseColumn[] = []
  const seen = new Set<string>()
  for (const session of report.sessions) {
    if (!seen.has(session.courseId)) {
      seen.add(session.courseId)
      columns.push({ courseId: session.courseId, courseName: session.courseName })
    }
  }
  return columns
}
