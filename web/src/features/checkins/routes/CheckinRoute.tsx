import { useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { courseIdSchema } from '@/features/courses'
import { sessionIdSchema, useCourseSessionsQuery } from '@/features/sessions'
import { useCheckinMutation, useCheckinsQuery, useSessionSnapshotQuery } from '../api/checkin.queries'
import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { useGivePointsMutation } from '../api/points.queries'
import { useSessionQr } from '../hooks/useSessionQr'
import { checkinsToCsv, downloadCsv } from '../lib/csv'
import { GivePointsDialog } from '../components/GivePointsDialog'
import { QrDialog } from '../components/QrDialog'
import { StudentCheckinTable } from '../components/StudentCheckinTable'
import { AsyncPage } from '@/shared/ui/AsyncPage'
import { BackLink } from '@/shared/ui/BackLink'
import { Button } from '@/shared/ui/Button'
import { Field } from '@/shared/ui/Field'
import { FreshnessBadge } from '@/shared/ui/FreshnessBadge'
import { Input } from '@/shared/ui/Input'
import { PageHeader } from '@/shared/ui/PageHeader'
import { Pagination } from '@/shared/ui/Pagination'
import { Select } from '@/shared/ui/Select'
import { StatsGrid } from '@/shared/ui/StatsGrid'
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
  const checkinMutation = useCheckinMutation(courseId, sessionId)
  const givePoints = useGivePointsMutation(courseId, sessionId)
  const [pointsTarget, setPointsTarget] = useState<StudentCheckin | null>(null)
  // The dialog restores focus to the element that opened it, so we deliberately
  // do not call .focus() ourselves.
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
  const handleToggle = (studentId: StudentId, checkedIn: boolean) => {
    checkinMutation.mutate(
      {
        studentId,
        checkedIn,
        expectedSnapshotVersion: snapshotQuery.data?.version,
        idempotencyKey: crypto.randomUUID(),
      },
      {
        onError: (error) => announce(`${getErrorMessage(error)} The check-in was restored.`, 'error'),
        onSuccess: () => announce(
          checkedIn ? 'Check-in confirmed in Humanix.' : 'Check-in removed from Humanix.',
          'success',
        ),
      },
    )
  }
  const handleGivePoints = (points: number) => {
    if (pointsTarget === null) {
      return
    }
    givePoints.mutate(
      {
        studentId: pointsTarget.student_id,
        points,
        expectedPoints: pointsTarget.participation_points,
      },
      {
        onSuccess: (result) => {
          announce(`${result.pointsAwarded} point(s) awarded to ${pointsTarget.name}.`, 'success')
          setPointsTarget(null)
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
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
              pendingStudentId={checkinMutation.isPending ? checkinMutation.variables.studentId : undefined}
              pendingPointsStudentId={givePoints.isPending ? givePoints.variables.studentId : undefined}
              onToggle={handleToggle}
              onGivePoints={setPointsTarget}
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
        upstreamAttendanceLabel={qr.upstreamAttendanceLabel}
        upstreamVerifiedAt={qr.upstreamVerifiedAt}
        upstreamVerificationError={qr.upstreamVerificationError}
      />
      <GivePointsDialog
        open={pointsTarget !== null}
        studentName={pointsTarget?.name ?? ''}
        pending={givePoints.isPending}
        errorMessage={givePoints.error === null ? undefined : getErrorMessage(givePoints.error)}
        onClose={() => setPointsTarget(null)}
        onSubmit={handleGivePoints}
      />
    </section>
  )
}
