import { useMemo } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { courseIdSchema } from '@/features/courses'
import { sessionIdSchema, useCourseSessionsQuery } from '@/features/sessions'
import { useCheckinsQuery, useSessionSnapshotQuery, useToggleCheckinMutation } from '../api/checkin.queries'
import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { useSessionQr } from '../hooks/useSessionQr'
import { checkinsToCsv, downloadCsv } from '../lib/csv'
import { QrDialog } from '../components/QrDialog'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { Avatar } from '@/shared/ui/Avatar'
import { BackLink } from '@/shared/ui/BackLink'
import { Badge } from '@/shared/ui/Badge'
import { Button } from '@/shared/ui/Button'
import { Field } from '@/shared/ui/Field'
import { FreshnessBadge } from '@/shared/ui/FreshnessBadge'
import { Input } from '@/shared/ui/Input'
import { PageHeader } from '@/shared/ui/PageHeader'
import { Pagination } from '@/shared/ui/Pagination'
import { Select } from '@/shared/ui/Select'
import { StatsGrid } from '@/shared/ui/StatsGrid'
import { Table, TableContainer } from '@/shared/ui/Table'
import { getErrorMessage } from '@/shared/lib/errors'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import { useToast } from '@/shared/ui/Toast'
import '../checkins.css'

const PAGE_SIZE = 25
const filterValues = ['all', 'checked', 'not-checked'] as const
type CheckinFilter = (typeof filterValues)[number]

function isCheckinFilter(value: string | null): value is CheckinFilter {
  return filterValues.some((filter) => filter === value)
}

export function Component() {
  const routeParams = useParams()
  const courseIdResult = courseIdSchema.safeParse(routeParams['courseId'])
  const sessionIdResult = sessionIdSchema.safeParse(routeParams['sessionId'])
  if (!courseIdResult.success || !sessionIdResult.success) {
    throw new Response('Session not found', { status: 404 })
  }
  const courseId = courseIdResult.data
  const sessionId = sessionIdResult.data
  const [params, setParams] = useSearchParams()
  const checkinsQuery = useCheckinsQuery(courseId, sessionId)
  const snapshotQuery = useSessionSnapshotQuery(courseId, sessionId)
  const courseQuery = useCourseSessionsQuery(courseId)
  const qr = useSessionQr(courseId, sessionId)
  const toggleCheckin = useToggleCheckinMutation(courseId, sessionId)
  const connected = useConnectionStore((state) => state.status === 'connected')
  const { announce } = useToast()

  const students = useMemo(
    () => checkinsQuery.data?.students ?? [],
    [checkinsQuery.data],
  )
  const search = params.get('search') ?? ''
  const rawFilter = params.get('status')
  const filter: CheckinFilter = isCheckinFilter(rawFilter) ? rawFilter : 'all'
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isSafeInteger(rawPage) && rawPage > 0 ? rawPage : 1

  const filteredStudents = useMemo(() => {
    const normalizedSearch = search.toLowerCase()
    return students.filter((student) => {
      const matchesSearch = [student.name, student.nickname, student.student_id]
        .some((value) => value.toLowerCase().includes(normalizedSearch))
      const matchesStatus =
        filter === 'all' ||
        (filter === 'checked' && student.checked_in) ||
        (filter === 'not-checked' && !student.checked_in)
      return matchesSearch && matchesStatus
    })
  }, [filter, search, students])
  const totalPages = Math.max(1, Math.ceil(filteredStudents.length / PAGE_SIZE))
  const safePage = Math.min(page, totalPages)
  const pagedStudents = filteredStudents.slice(
    (safePage - 1) * PAGE_SIZE,
    safePage * PAGE_SIZE,
  )
  const checkedCount = students.filter((student) => student.checked_in).length

  const setParam = (key: string, value: string, defaultValue = '') => {
    const next = new URLSearchParams(params)
    if (value === defaultValue) {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    if (key !== 'page') {
      next.delete('page')
    }
    setParams(next, { replace: true })
  }
  const handleToggle = (studentId: StudentId, checked: boolean) => {
    toggleCheckin.mutate(
      { studentId, checked },
      {
        onError: (error) => announce(`${getErrorMessage(error)} The check-in was restored.`, 'error'),
        onSuccess: () => announce('Check-in updated.', 'success'),
      },
    )
  }
  const queryError = checkinsQuery.error ?? courseQuery.error

  return (
    <section>
      <BackLink to={`/courses/${courseId}/sessions`}>Back to sessions</BackLink>
      <PageHeader
        eyebrow="Live check-in"
        title={checkinsQuery.data?.name ?? 'Session'}
        description={courseQuery.data?.name ?? 'Manage student attendance and the session QR code.'}
        actions={
          <>
            <Button loading={qr.refreshing} onClick={qr.openQr}>
              View QR code
            </Button>
            <Button
              onClick={() => downloadCsv(checkinsToCsv(students), `checkin_${sessionId}.csv`)}
            >
              Export CSV
            </Button>
          </>
        }
      />
      <AsyncPage
        pending={checkinsQuery.isPending}
        fetching={checkinsQuery.isFetching}
        error={queryError === null ? null : getErrorMessage(queryError)}
        empty={students.length === 0}
        emptyTitle="No students enrolled"
        emptyDescription="Students will appear after the session roster synchronizes."
        onRetry={() => void checkinsQuery.refetch()}
      >
        <StatsGrid
          stats={[
            { label: 'Checked in', value: `${checkedCount}/${students.length}` },
            {
              label: 'Attendance rate',
              value: students.length === 0 ? '0%' : `${Math.round(checkedCount / students.length * 100)}%`,
            },
          ]}
        />
        <FreshnessBadge
          live={connected}
          generatedAt={snapshotQuery.data?.generatedAt}
          stale={snapshotQuery.data?.stale}
          quality={snapshotQuery.data?.quality}
        />
        <div className="filter-bar">
          <Field label="Search students">
            {(fieldProps) => (
              <Input
                {...fieldProps}
                type="search"
                value={search}
                onChange={(event) => setParam('search', event.target.value)}
              />
            )}
          </Field>
          <Field label="Check-in status">
            {(fieldProps) => (
              <Select
                {...fieldProps}
                value={filter}
                onChange={(event) => setParam('status', event.target.value, 'all')}
              >
                <option value="all">All students</option>
                <option value="checked">Checked in</option>
                <option value="not-checked">Not checked in</option>
              </Select>
            )}
          </Field>
        </div>
        {filteredStudents.length === 0 ? (
          <p className="no-results" role="status">No students match these filters.</p>
        ) : (
          <>
            <StudentCheckinTable
              students={pagedStudents}
              pendingStudentId={toggleCheckin.isPending ? toggleCheckin.variables.studentId : undefined}
              onToggle={handleToggle}
            />
            <Pagination
              page={safePage}
              pageSize={PAGE_SIZE}
              totalItems={filteredStudents.length}
              onPageChange={(nextPage) => setParam('page', String(nextPage), '1')}
            />
          </>
        )}
      </AsyncPage>
      <QrDialog
        checkedCount={checkedCount}
        courseName={courseQuery.data?.name ?? undefined}
        errorMessage={qr.errorMessage}
        expiresAt={qr.expiresAt}
        onClose={qr.closeQr}
        onRefresh={qr.refresh}
        open={qr.open}
        qrUrl={qr.qrUrl}
        refreshing={qr.refreshing}
        sessionName={checkinsQuery.data?.name}
        totalCount={students.length}
        warningMessage={qr.warningMessage}
      />
    </section>
  )
}

function StudentCheckinTable({
  students,
  pendingStudentId,
  onToggle,
}: {
  readonly students: readonly StudentCheckin[]
  readonly pendingStudentId?: StudentId | undefined
  readonly onToggle: (studentId: StudentId, checked: boolean) => void
}) {
  return (
    <TableContainer>
      <Table>
        <thead>
          <tr>
            <th scope="col">Student</th>
            <th className="is-secondary" scope="col">School</th>
            <th className="is-mobile-secondary" scope="col">Status</th>
            <th className="is-secondary" scope="col">Points</th>
            <th scope="col"><span className="sr-only">Action</span></th>
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
              <td>
                <Button
                  loading={pendingStudentId === student.student_id}
                  size="sm"
                  variant={student.checked_in ? 'secondary' : 'primary'}
                  onClick={() => onToggle(student.student_id, !student.checked_in)}
                >
                  {student.checked_in ? 'Undo' : 'Check in'}
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </TableContainer>
  )
}
