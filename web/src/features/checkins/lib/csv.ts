import type { StudentCheckin } from '../api/checkin.schemas'

function csvCell(value: string | number): string {
  const text = String(value)
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text
}

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

export function downloadCsv(contents: string, fileName: string): void {
  const blob = new Blob([contents], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.click()
  URL.revokeObjectURL(url)
}
