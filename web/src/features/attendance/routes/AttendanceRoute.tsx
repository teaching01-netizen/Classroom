import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { courseIdSchema } from '@/features/courses'
import { useAttendanceQuery, useAttendanceSnapshotQuery } from '../api/attendance.queries'
import { downloadAttendanceReport, type AttendanceExportFormat } from '../api/attendance.exports'
import { attendancePercent } from '../lib/attendance'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { Avatar } from '@/shared/ui/Avatar'
import { BackLink } from '@/shared/ui/BackLink'
import { Badge } from '@/shared/ui/Badge'
import { Button } from '@/shared/ui/Button'
import { FreshnessBadge } from '@/shared/ui/FreshnessBadge'
import { PageHeader } from '@/shared/ui/PageHeader'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { Table, TableContainer } from '@/shared/ui/Table'
import { getErrorMessage } from '@/shared/lib/errors'
import '../attendance.css'

export function Component() {
  const params = useParams()
  const courseIdResult = courseIdSchema.safeParse(params['courseId'])
  if (!courseIdResult.success) {
    throw new Response('Course not found', { status: 404 })
  }
  const courseId = courseIdResult.data
  const query = useAttendanceQuery(courseId)
  const snapshotQuery = useAttendanceSnapshotQuery(courseId)
  const report = query.data
  const atRiskCount = report?.students.filter((student) => student.atRisk).length ?? 0
  const [exporting, setExporting] = useState<AttendanceExportFormat | null>(null)
  const [exportError, setExportError] = useState<string | null>(null)
  const exportingRef = useRef<AttendanceExportFormat | null>(null)
  const focusTriggerRef = useRef<HTMLElement | null>(null)
  const exportDisabled = query.isFetching || exporting !== null

  useEffect(() => {
    // Once the export finishes and the buttons are re-enabled, return focus to
    // the control that initiated it.
    if (exporting !== null || focusTriggerRef.current === null) {
      return
    }
    const trigger = focusTriggerRef.current
    focusTriggerRef.current = null
    trigger.focus()
  }, [exporting])

  async function exportReport(format: AttendanceExportFormat): Promise<void> {
    if (exportingRef.current !== null) {
      return
    }
    exportingRef.current = format
    focusTriggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setExporting(format)
    setExportError(null)
    try {
      await downloadAttendanceReport({
        courseId,
        format,
        threshold: report?.threshold ?? 0,
      })
      await query.refetch()
    } catch (error: unknown) {
      setExportError(getErrorMessage(error))
    } finally {
      exportingRef.current = null
      setExporting(null)
    }
  }

  return (
    <section>
      <BackLink to={`/courses/${courseId}/sessions`}>Back to sessions</BackLink>
      <PageHeader
        eyebrow="Attendance"
        title={report?.courseName ?? 'Attendance report'}
        description="Per-student attendance across completed sessions. Active and not-started sessions are excluded from risk calculations."
        status={(
          <FreshnessBadge
            generatedAt={snapshotQuery.data?.generatedAt}
            stale={snapshotQuery.data?.stale}
            quality={snapshotQuery.data?.quality}
          />
        )}
        actions={(
          <>
            <Button loading={query.isFetching} onClick={() => void query.refetch()}>
              Refresh report
            </Button>
            <Button
              disabled={exportDisabled}
              loading={exporting === 'csv'}
              onClick={() => void exportReport('csv')}
            >
              Export CSV
            </Button>
            <Button
              disabled={exportDisabled}
              loading={exporting === 'xlsx'}
              onClick={() => void exportReport('xlsx')}
              variant="primary"
            >
              Export Excel
            </Button>
          </>
        )}
      />
      {(query.isFetching || exporting !== null) && (
        <div aria-live="polite" className="sync-indicator" role="status">
          Refreshing latest data…
        </div>
      )}
      {report?.stale === true && (
        <p className="notice notice--warning" role="status">
          This stored attendance report may be out of date. Refresh or export to validate the latest data.
        </p>
      )}
      {exportError !== null && (
        <p className="notice notice--danger" role="alert">{exportError}</p>
      )}
      <AsyncPage
        pending={query.isPending}
        fetching={false}
        error={query.error === null ? null : getErrorMessage(query.error)}
        empty={report?.students.length === 0}
        emptyTitle="No attendance data"
        emptyDescription="Completed session attendance will appear here."
        onRetry={() => void query.refetch()}
      >
        {report !== undefined && (
          <>
            <StatsGrid
              stats={[
                { label: 'Students', value: report.students.length },
                { label: 'Sessions', value: report.sessions.length },
                {
                  label: 'At risk',
                  value: atRiskCount,
                  tone: atRiskCount > 0 ? 'warning' : 'positive',
                },
                { label: 'Computed in', value: `${(report.durationMs / 1_000).toFixed(1)}s` },
              ]}
            />
            {report.truncated && (
              <p className="notice notice--warning" role="status">
                This report reached its time limit, so some sessions may be missing.
              </p>
            )}
            {report.errors.length > 0 && (
              <div className="notice notice--danger" role="alert">
                <strong>{report.errors.length} sessions could not be loaded.</strong>
                <ul>
                  {report.errors.map((error) => (
                    <li key={error.sessionId}>{error.sessionId}: {error.reason}</li>
                  ))}
                </ul>
              </div>
            )}
            <TableContainer>
              <Table>
                <thead>
                  <tr>
                    <th scope="col">Student</th>
                    {report.sessions.map((session) => (
                      <th className="attendance-session-column" key={session.session_id} scope="col">
                        S{session.session_number}
                      </th>
                    ))}
                    <th scope="col">Attended</th>
                    <th scope="col">Rate</th>
                  </tr>
                </thead>
                <tbody>
                  {report.students.map((student) => {
                    const percent = attendancePercent(student.attendanceRate)
                    return (
                      <tr key={student.studentId}>
                        <td>
                          <span className="student-cell">
                            <Avatar name={student.name} src={student.avatarUrl} />
                            <span>
                              <strong>{student.nickname || student.name}</strong>
                              <span>{student.studentId}</span>
                            </span>
                          </span>
                        </td>
                        {student.perSession.map((cell) => (
                          <td
                            className="attendance-session-column attendance-cell"
                            key={cell.sessionId}
                            title={cell.sessionName}
                          >
                            {cell.sessionStatus === 'active' || cell.sessionStatus === 'not_started'
                              ? '—'
                              : cell.status === 'error'
                                ? '!'
                                : cell.checkedIn
                                  ? '✓'
                                  : '×'}
                          </td>
                        ))}
                        <td>{student.attendedSessions}/{student.totalSessions}</td>
                        <td><Badge tone={student.atRisk ? 'danger' : 'success'}>{percent}%</Badge></td>
                      </tr>
                    )
                  })}
                </tbody>
              </Table>
            </TableContainer>
          </>
        )}
      </AsyncPage>
    </section>
  )
}
