import { attendancePercent } from '@/features/attendance'
import { csvCell, downloadCsv } from '@/shared/lib/csv'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { absenceRate } from './absence'
import { buildCourseColumns } from './course-columns'

export { downloadCsv }

export function absenceDashboardToCsv(report: AbsenceDashboard): string {
  const courses = buildCourseColumns(report)
  const header: readonly (string | number)[] = [
    'WCode',
    'Name',
    'Nickname',
    'School',
    ...courses.flatMap((course) => [
      `${course.courseName} absences`,
      `${course.courseName} sessions`,
      `${course.courseName} absence rate`,
    ]),
    'Total absences',
    'Attended',
    'Total sessions',
    'Rate',
  ]
  const rows = report.students.map((student) => {
    const courseCells = courses.flatMap((course) => {
      const courseAbsence = student.courses.find((item) => item.courseId === course.courseId)
      // Students who do not study a course leave all three cells empty.
      if (courseAbsence === undefined) {
        return ['', '', '']
      }
      return [
        courseAbsence.absences,
        courseAbsence.totalSessions,
        `${absenceRate(courseAbsence.absences, courseAbsence.totalSessions)}%`,
      ]
    })
    return [
      student.studentId,
      student.name,
      student.nickname,
      student.school,
      ...courseCells,
      student.totalSessions - student.attendedSessions,
      student.attendedSessions,
      student.totalSessions,
      `${attendancePercent(student.attendanceRate)}%`,
    ]
  })
  // BOM so spreadsheet apps read UTF-8 names correctly.
  return '\ufeff' + [header, ...rows].map((row) => row.map(csvCell).join(',')).join('\n')
}

export function downloadAbsenceDashboard(report: AbsenceDashboard): void {
  downloadCsv(absenceDashboardToCsv(report), `absence-dashboard-${new Date().toISOString().slice(0, 10)}.csv`)
}
