import { csvCell, downloadCsv } from '@/shared/lib/csv'
import type { StudentCheckin } from '../api/checkin.schemas'

export { downloadCsv }

export function checkinsToCsv(students: readonly StudentCheckin[]): string {
  const rows: readonly (readonly (string | number)[])[] = [
    ['WCode', 'Name', 'Nickname', 'School', 'Status', 'Points'],
    ...students.map((student) => [
      student.student_id,
      student.name,
      student.nickname,
      student.school,
      student.checked_in ? 'Checked In' : 'Not Checked',
      student.participation_points,
    ]),
  ]
  return rows.map((row) => row.map(csvCell).join(',')).join('\n')
}
