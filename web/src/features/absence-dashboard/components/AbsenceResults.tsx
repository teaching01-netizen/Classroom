import { Fragment, useMemo, useState } from 'react'
import { attendancePercent } from '@/features/attendance'
import type { AbsenceDashboard, StudentAbsence } from '../api/absence.schemas'
import { absenceRate } from '../lib/absence'
import { buildCourseColumns } from '../lib/course-columns'
import { Avatar } from '@/shared/ui/Avatar'
import { Badge } from '@/shared/ui/Badge'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { Table, TableContainer } from '@/shared/ui/Table'

type AbsenceResultsProps = {
  readonly report: AbsenceDashboard
}

function absenceForCourse(student: StudentAbsence, courseId: string) {
  return student.courses.find((course) => course.courseId === courseId)
}

type MissedGroup = {
  readonly courseName: string
  readonly items: StudentAbsence['perSession']
}

function missedSessionsByCourse(
  student: StudentAbsence,
  sessions: AbsenceDashboard['sessions'],
): MissedGroup[] {
  const courseBySession = new Map(sessions.map((session) => [session.sessionId, session]))
  const byCourse = new Map<string, MissedGroup>()
  for (const item of student.perSession) {
    if (item.status !== 'absent') {
      continue
    }
    const session = courseBySession.get(item.sessionId)
    const courseId = session?.courseId ?? 'other'
    const courseName = session?.courseName ?? 'Other'
    let group = byCourse.get(courseId)
    if (group === undefined) {
      group = { courseName, items: [] }
      byCourse.set(courseId, group)
    }
    group.items.push(item)
  }
  return Array.from(byCourse.values())
    .map((group) => ({
      ...group,
      items: [...group.items].sort((a, b) =>
        (a.sessionDate || '').localeCompare(b.sessionDate || '')
        || a.sessionNumber - b.sessionNumber,
      ),
    }))
    .sort((a, b) => a.courseName.localeCompare(b.courseName))
}

function AbsenceDetail({
  student,
  sessions,
}: {
  readonly student: StudentAbsence
  readonly sessions: AbsenceDashboard['sessions']
}) {
  const groups = useMemo(() => missedSessionsByCourse(student, sessions), [student, sessions])
  return (
    <div className="absence-detail">
      {groups.map((group) => (
        <div className="absence-detail__course" key={group.courseName}>
          <h4>
            {group.courseName}
            <span> · {group.items.length} session{group.items.length === 1 ? '' : 's'} missed</span>
          </h4>
          <ul>
            {group.items.map((item) => (
              <li key={item.sessionId}>
                S{item.sessionNumber} · {item.sessionName || `Session ${item.sessionNumber}`} · {item.sessionDate || '—'}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

export function AbsenceResults({ report }: AbsenceResultsProps) {
  const courseColumns = useMemo(() => buildCourseColumns(report), [report])
  const [expandedStudents, setExpandedStudents] = useState<ReadonlySet<string>>(new Set())

  const toggleStudent = (studentId: string) => {
    setExpandedStudents((previous) => {
      const next = new Set(previous)
      if (next.has(studentId)) {
        next.delete(studentId)
      } else {
        next.add(studentId)
      }
      return next
    })
  }

  const detailColSpan = courseColumns.length + 3
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
          <caption className="absence-legend">
            Each cell shows absences out of the total sessions in that course and the absence rate.
            A dash (—) means the student does not study the course. Click the chevron next to a
            student to see which sessions they missed.
          </caption>
          <thead>
            <tr>
              <th scope="col">Student</th>
              {courseColumns.map((course) => (
                <th className="absence-course-column" key={course.courseId} scope="col">
                  {course.courseName}
                </th>
              ))}
              <th scope="col">Attended</th>
              <th scope="col">Rate</th>
            </tr>
          </thead>
          <tbody>
            {report.students.map((student) => {
              const displayName = student.nickname || student.name
              const hasMissed = student.perSession.some((item) => item.status === 'absent')
              const expanded = expandedStudents.has(student.studentId)
              const detailId = `absence-detail-${student.studentId}`
              return (
                <Fragment key={student.studentId}>
                  <tr>
                    <td>
                      <span className="student-cell">
                        {hasMissed ? (
                          <button
                            aria-controls={detailId}
                            aria-expanded={expanded}
                            aria-label={`${expanded ? 'Hide' : 'Show'} missed sessions for ${displayName}`}
                            className="absence-expand-toggle"
                            type="button"
                            onClick={() => toggleStudent(student.studentId)}
                          >
                            <svg
                              aria-hidden="true"
                              className="absence-expand-toggle__icon"
                              fill="none"
                              height="14"
                              stroke="currentColor"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              strokeWidth="2"
                              viewBox="0 0 24 24"
                              width="14"
                            >
                              <path d="m6 9 6 6 6-6" />
                            </svg>
                          </button>
                        ) : (
                          <span aria-hidden="true" className="absence-expand-spacer" />
                        )}
                        <Avatar name={student.name} src={student.avatarUrl} />
                        <span>
                          <strong>{displayName}</strong>
                          <span>{student.studentId} · {student.school || 'School unavailable'}</span>
                        </span>
                      </span>
                    </td>
                    {courseColumns.map((course) => {
                      const courseAbsence = absenceForCourse(student, course.courseId)
                      return (
                        <td className="absence-course-cell" key={course.courseId}>
                          {courseAbsence === undefined ? (
                            <span
                              className="absence-course-cell__none"
                              title={`${course.courseName}: student does not study this course`}
                            >
                              —
                            </span>
                          ) : (
                            <span className="absence-course-cell__stack">
                              <span
                                className={courseAbsence.absences > 0 ? 'is-missing' : 'is-clear'}
                                title={
                                  courseAbsence.absences === 0
                                    ? `${course.courseName}: attended every session`
                                    : `${course.courseName}: ${courseAbsence.absences} of ${courseAbsence.totalSessions} sessions absent`
                                }
                              >
                                {courseAbsence.absences}/{courseAbsence.totalSessions}
                              </span>
                              <span className="absence-course-cell__meta">
                                {absenceRate(courseAbsence.absences, courseAbsence.totalSessions)}% absent
                              </span>
                            </span>
                          )}
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
                  {expanded && (
                    <tr className="absence-detail-row" id={detailId}>
                      <td colSpan={detailColSpan}>
                        <AbsenceDetail student={student} sessions={report.sessions} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </Table>
      </TableContainer>
      <p className="generated-at">Generated {new Date(report.generatedAt).toLocaleString()}</p>
    </div>
  )
}
