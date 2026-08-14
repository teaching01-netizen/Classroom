import { csvCell, downloadCsv } from '@/shared/lib/csv'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { absenceRate } from './absence'

export { downloadCsv }

const header = [
  'WCode',
  'Name',
  'Nickname',
  'School',
  'Course',
  'Total sessions',
  'Absences',
  'Absence rate',
] as const

export function absenceDashboardToCsv(report: AbsenceDashboard): string {
  // One row per student per course they take — the same per-course breakdown
  // the dashboard shows, without empty cells for courses they do not study.
  const rows: (readonly (string | number)[])[] = []
  for (const student of report.students) {
    const courses = [...student.courses].sort((a, b) =>
      a.courseName.localeCompare(b.courseName),
    )
    for (const course of courses) {
      rows.push([
        student.studentId,
        student.name,
        student.nickname,
        student.school,
        course.courseName,
        course.totalSessions,
        course.absences,
        `${absenceRate(course.absences, course.totalSessions)}%`,
      ])
    }
  }
  // BOM so spreadsheet apps read UTF-8 names correctly.
  return '\ufeff' + [header, ...rows].map((row) => row.map(csvCell).join(',')).join('\n')
}

export function downloadAbsenceDashboard(report: AbsenceDashboard): void {
  downloadCsv(absenceDashboardToCsv(report), `absence-dashboard-${new Date().toISOString().slice(0, 10)}.csv`)
}
