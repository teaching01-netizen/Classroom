import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { Avatar } from '@/shared/ui/Avatar'
import { Badge } from '@/shared/ui/Badge'
import { Button } from '@/shared/ui/Button'
import { Table, TableContainer } from '@/shared/ui/Table'

export type StudentCheckinTableProps = {
  readonly students: readonly StudentCheckin[]
  readonly pendingStudentId?: StudentId | undefined
  readonly pendingPointsStudentId?: StudentId | undefined
  readonly onToggle: (studentId: StudentId, checked: boolean) => void
  readonly onGivePoints: (student: StudentCheckin) => void
}

// The give-points control is rendered from `can_give_point` alone. It is
// deliberately NOT derived from `checked_in` or `participation_points`:
// Warwick owns eligibility, and R2/R2prime in the test file assert both
// directions of that implication.
export function StudentCheckinTable({
  students,
  pendingStudentId,
  pendingPointsStudentId,
  onToggle,
  onGivePoints,
}: StudentCheckinTableProps) {
  return (
    <TableContainer>
      <Table>
        <thead>
          <tr>
            <th scope="col">Student</th>
            <th className="is-secondary" scope="col">School</th>
            <th className="is-mobile-secondary" scope="col">Status</th>
            <th className="is-secondary" scope="col">Points</th>
            <th className="is-secondary" scope="col">Give points</th>
            <th scope="col"><span className="sr-only">Check-in action</span></th>
          </tr>
        </thead>
        <tbody>
          {students.map((student) => (
            <tr key={student.student_id}>
              <td>
                <span className="student-cell">
                  <Avatar name={student.name} src={student.avatar_url} />
                  <span>
                    <strong>{student.nickname || student.name}</strong>
                    <span>{student.student_id}</span>
                  </span>
                </span>
              </td>
              <td className="is-secondary">{student.school || '—'}</td>
              <td className="is-mobile-secondary">
                <Badge tone={student.checked_in ? 'success' : 'neutral'}>
                  {student.checked_in ? 'Checked in' : 'Not checked in'}
                </Badge>
              </td>
              <td className="is-secondary">{student.participation_points}</td>
              <td className="is-secondary">
                {student.can_give_point ? (
                  <Button
                    aria-label={`Give points to ${student.name}`}
                    loading={pendingPointsStudentId === student.student_id}
                    size="sm"
                    variant="primary"
                    onClick={() => onGivePoints(student)}
                  >
                    + Points
                  </Button>
                ) : (
                  <span aria-hidden="true">—</span>
                )}
              </td>
              <td>
                <Button
                  loading={pendingStudentId === student.student_id}
                  size="sm"
                  variant={student.checked_in ? 'secondary' : 'primary'}
                  onClick={() => onToggle(student.student_id, !student.checked_in)}
                >
                  {student.checked_in ? 'Undo check-in' : 'Check in'}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </TableContainer>
  )
}
