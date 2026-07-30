import { attendancePercent } from '@/features/attendance'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { Avatar } from '@/shared/ui/Avatar'
import { Badge } from '@/shared/ui/Badge'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { Table, TableContainer } from '@/shared/ui/Table'

type AbsenceResultsProps = {
  readonly report: AbsenceDashboard
}

export function AbsenceResults({ report }: AbsenceResultsProps) {
  return (
    <div className="stack">
      <StatsGrid
        stats={[
          { label: 'Students', value: report.totalStudents },
          { label: 'Courses', value: report.totalCourses },
          { label: 'Average attendance', value: `${attendancePercent(report.avgAttendanceRate)}%` },
          {
            label: 'At risk',
            value: report.atRiskCount,
            tone: report.atRiskCount > 0 ? 'warning' : 'positive',
          },
        ]}
      />
      <TableContainer>
        <Table>
          <thead>
            <tr>
              <th scope="col">Student</th>
              {report.sessions.map((session) => (
                <th className="attendance-session-column" key={session.sessionId} scope="col">
                  <span title={`${session.courseName}: ${session.name}`}>S{session.sessionNumber}</span>
                </th>
              ))}
              <th scope="col">Attended</th>
              <th scope="col">Rate</th>
            </tr>
          </thead>
          <tbody>
            {report.students.map((student) => (
              <tr key={student.studentId}>
                <td>
                  <span className="student-cell">
                    <Avatar name={student.name} src={student.avatarUrl} />
                    <span>
                      <strong>{student.nickname || student.name}</strong>
                      <span>{student.studentId} · {student.school || 'School unavailable'}</span>
                    </span>
                  </span>
                </td>
                {report.sessions.map((session) => {
                  const checkin = student.perSession.find((item) => item.sessionId === session.sessionId)
                  return (
                    <td
                      className="attendance-session-column attendance-cell"
                      key={session.sessionId}
                      title={`${session.courseName}: ${session.name}`}
                    >
                      {checkin === undefined || checkin.sessionStatus !== 'done'
                        ? '—'
                        : checkin.status === 'error'
                          ? '!'
                          : checkin.checkedIn
                            ? '✓'
                            : '×'}
                    </td>
                  )
                })}
                <td>{student.attendedSessions}/{student.totalSessions}</td>
                <td>
                  <Badge tone={student.atRisk ? 'danger' : 'success'}>
                    {attendancePercent(student.attendanceRate)}%
                  </Badge>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      </TableContainer>
      <p className="generated-at">Generated {new Date(report.generatedAt).toLocaleString()}</p>
    </div>
  )
}
