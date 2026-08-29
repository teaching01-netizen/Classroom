import { Link, useNavigate, useParams } from 'react-router-dom'
import { courseIdSchema } from '@/features/courses'
import { useCourseSessionsQuery } from '../api/session.queries'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { BackLink } from '@/shared/ui/BackLink'
import { Badge, type BadgeTone } from '@/shared/ui/Badge'
import { PageHeader } from '@/shared/ui/PageHeader'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { Table, TableContainer } from '@/shared/ui/Table'
import { getErrorMessage } from '@/shared/lib/errors'
import type { SessionSummary } from '../api/session.schemas'
import '../sessions.css'

const statusTone: Readonly<Record<SessionSummary['status'], BadgeTone>> = {
  not_started: 'neutral',
  active: 'info',
  done: 'success',
  auth_error: 'danger',
}

export function Component() {
  const params = useParams()
  const courseIdResult = courseIdSchema.safeParse(params['courseId'])
  if (!courseIdResult.success) {
    throw new Response('Course not found', { status: 404 })
  }
  const courseId = courseIdResult.data
  const navigate = useNavigate()
  const query = useCourseSessionsQuery(courseId)
  const sessions = query.data?.sessions ?? []
  const stats = [
    { label: 'Total sessions', value: sessions.length },
    { label: 'Completed', value: sessions.filter((session) => session.status === 'done').length },
    { label: 'Students', value: query.data?.enrolled_count ?? '—' },
    {
      label: 'Average attendance',
      value: query.data === undefined ? '—' : `${Math.round(query.data.avg_attendance_rate * 100)}%`,
    },
  ] as const

  return (
    <section>
      <BackLink to="/">Back to dashboard</BackLink>
      <PageHeader
        eyebrow="Sessions"
        title={query.data?.name ?? 'Course sessions'}
        description="Open a session to display its QR code and manage student check-ins."
        actions={<Link className="button-link" to={`/courses/${courseId}/attendance`}>Attendance report</Link>}
      />
      <AsyncPage
        pending={query.isPending}
        fetching={query.isFetching}
        error={query.error === null ? null : getErrorMessage(query.error)}
        empty={sessions.length === 0}
        emptyTitle="No sessions found"
        emptyDescription="Attendance sessions will appear after the course schedule synchronizes."
        onRetry={() => void query.refetch()}
      >
        <StatsGrid stats={stats} />
        <TableContainer>
          <Table>
            <thead>
              <tr>
                <th scope="col">Session</th>
                <th className="is-secondary" scope="col">Date</th>
                <th scope="col">Status</th>
                <th className="is-secondary" scope="col">Checked in</th>
                <th scope="col"><span className="sr-only">Open</span></th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((session) => (
                <tr
                  className="session-row"
                  key={session.session_id}
                  onClick={(event) => {
                    // Let clicks on the session-name link handle themselves;
                    // everything else on the row navigates to the session.
                    if ((event.target as HTMLElement).closest('a') !== null) return
                    navigate(`/courses/${courseId}/sessions/${session.session_id}`)
                  }}
                >
                  <td>
                    <Link
                      className="session-card-link"
                      to={`/courses/${courseId}/sessions/${session.session_id}`}
                    >
                      <strong>{session.name}</strong>
                      <span className="table-subtitle">
                        Session {session.session_number}
                        <span className="session-checkin-summary">
                          {' '}· {session.checked_in_count}/{session.total_students} checked in
                        </span>
                      </span>
                    </Link>
                  </td>
                  <td className="is-secondary">{session.date}</td>
                  <td><Badge tone={statusTone[session.status]}>{session.status.replace('_', ' ')}</Badge></td>
                  <td className="is-secondary">{session.checked_in_count}/{session.total_students}</td>
                  <td>
                    <Link
                      aria-label={`Open ${session.name}`}
                      className="session-open-affordance"
                      to={`/courses/${courseId}/sessions/${session.session_id}`}
                    >
                      Open
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </TableContainer>
      </AsyncPage>
    </section>
  )
}
