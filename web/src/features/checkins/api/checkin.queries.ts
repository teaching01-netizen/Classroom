import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient, type SnapshotVersionInfo } from '@/shared/api/api-client'
import { ApiError } from '@/shared/api/api-error'
import { endpoints } from '@/shared/api/endpoints'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import { applyCheckinDelta } from '@/shared/realtime/checkin-update'
import type { CourseId } from '@/features/courses'
import { sessionKeys, type SessionId } from '@/features/sessions'
import {
  sessionDetailSchema,
  checkinMutationSchema,
  type SessionDetail,
  type StudentId,
} from './checkin.schemas'

type CheckinVariables = {
  readonly studentId: StudentId
  readonly checkedIn: boolean
  readonly expectedSnapshotVersion?: number | undefined
  readonly idempotencyKey: string
}

// The cached session-detail entry: the parsed detail plus the server's
// snapshot envelope. Both hooks below share one query key and queryFn, so the
// detail and its freshness metadata always come from the same fetch.
type SessionQueryResult = {
  readonly detail: SessionDetail
  readonly snapshot: SnapshotVersionInfo | undefined
}

function fetchSession(courseId: CourseId, sessionId: SessionId, signal?: AbortSignal) {
  return apiClient
    .get(endpoints.session(courseId, sessionId), {
      schema: sessionDetailSchema,
      ...(signal === undefined ? {} : { signal }),
      includeSnapshot: true,
    })
    .then((result): SessionQueryResult => ({ detail: result.data, snapshot: result.snapshot }))
}

export function useCheckinsQuery(courseId: CourseId, sessionId: SessionId) {
  const connected = useConnectionStore((state) => state.status === 'connected')
  return useQuery({
    queryKey: sessionKeys.detail(courseId, sessionId),
    queryFn: ({ signal }) => fetchSession(courseId, sessionId, signal),
    select: (result: SessionQueryResult) => result.detail,
    refetchInterval: connected ? false : 10_000,
  })
}

export function useSessionSnapshotQuery(courseId: CourseId, sessionId: SessionId) {
  return useQuery({
    queryKey: sessionKeys.detail(courseId, sessionId),
    queryFn: ({ signal }) => fetchSession(courseId, sessionId, signal),
    select: (result: SessionQueryResult) => result.snapshot,
  })
}

export function useCheckinMutation(courseId: CourseId, sessionId: SessionId) {
  const queryClient = useQueryClient()
  const queryKey = sessionKeys.detail(courseId, sessionId)
  return useMutation({
    mutationFn: async ({
      studentId,
      checkedIn,
      expectedSnapshotVersion,
      idempotencyKey,
    }: CheckinVariables) => {
      const result = await apiClient.put(endpoints.checkin(courseId, sessionId, studentId), {
        body: { checkedIn, expectedSnapshotVersion, idempotencyKey },
        schema: checkinMutationSchema,
      })

      const verificationDelays = [0, 300, 900]
      for (const delayMs of verificationDelays) {
        if (delayMs > 0) {
          await new Promise((resolve) => window.setTimeout(resolve, delayMs))
        }
        const authoritative = await fetchSession(courseId, sessionId)
        queryClient.setQueryData(queryKey, authoritative)
        const student = authoritative.detail.students.find((item) => item.student_id === studentId)
        if (student?.checked_in === checkedIn) {
          return {
            ...result,
            status: result.status === 'pending_verification' ? 'confirmed' as const : result.status,
            refreshPending: false,
          }
        }
      }

      throw new ApiError('Humanix did not confirm the attendance change.', {
        kind: 'http',
        status: 502,
      })
    },
    onMutate: async ({ studentId, checkedIn }) => {
      await queryClient.cancelQueries({ queryKey })
      const previous = queryClient.getQueryData<SessionQueryResult>(queryKey)
      queryClient.setQueryData<SessionQueryResult>(queryKey, (current) =>
        current === undefined
          ? current
          : { ...current, detail: applyCheckinDelta(current.detail, studentId, checkedIn) },
      )
      return { previous }
    },
    onError: (_error, _variables, context) => {
      queryClient.setQueryData(queryKey, context?.previous)
    },
  })
}
