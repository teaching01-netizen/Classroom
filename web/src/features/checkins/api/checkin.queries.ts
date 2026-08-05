import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient, type SnapshotVersionInfo } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import { applyCheckinDelta } from '@/shared/realtime/checkin-update'
import type { CourseId } from '@/features/courses'
import { sessionKeys, type SessionId } from '@/features/sessions'
import {
  sessionDetailSchema,
  toggleCheckinSchema,
  type SessionDetail,
  type StudentId,
} from './checkin.schemas'

type ToggleCheckinVariables = {
  readonly studentId: StudentId
  readonly checked: boolean
}

// The cached session-detail entry: the parsed detail plus the server's
// snapshot envelope. Both hooks below share one query key and queryFn, so the
// detail and its freshness metadata always come from the same fetch.
type SessionQueryResult = {
  readonly detail: SessionDetail
  readonly snapshot: SnapshotVersionInfo | undefined
}

export function useCheckinsQuery(courseId: CourseId, sessionId: SessionId) {
  const connected = useConnectionStore((state) => state.status === 'connected')
  return useQuery({
    queryKey: sessionKeys.detail(courseId, sessionId),
    queryFn: ({ signal }) =>
      apiClient
        .get(endpoints.session(courseId, sessionId), {
          schema: sessionDetailSchema,
          signal,
          includeSnapshot: true,
        })
        .then((result): SessionQueryResult => ({ detail: result.data, snapshot: result.snapshot })),
    select: (result: SessionQueryResult) => result.detail,
    refetchInterval: connected ? false : 10_000,
  })
}

export function useSessionSnapshotQuery(courseId: CourseId, sessionId: SessionId) {
  return useQuery({
    queryKey: sessionKeys.detail(courseId, sessionId),
    queryFn: ({ signal }) =>
      apiClient
        .get(endpoints.session(courseId, sessionId), {
          schema: sessionDetailSchema,
          signal,
          includeSnapshot: true,
        })
        .then((result): SessionQueryResult => ({ detail: result.data, snapshot: result.snapshot })),
    select: (result: SessionQueryResult) => result.snapshot,
  })
}

export function useToggleCheckinMutation(courseId: CourseId, sessionId: SessionId) {
  const queryClient = useQueryClient()
  const queryKey = sessionKeys.detail(courseId, sessionId)
  return useMutation({
    mutationFn: ({ studentId, checked }: ToggleCheckinVariables) =>
      apiClient.post(endpoints.toggleCheckin(courseId, sessionId), {
        body: { student_id: studentId, checked },
        schema: toggleCheckinSchema,
      }),
    onMutate: async ({ studentId, checked }) => {
      await queryClient.cancelQueries({ queryKey })
      const previous = queryClient.getQueryData<SessionQueryResult>(queryKey)
      queryClient.setQueryData<SessionQueryResult>(queryKey, (current) =>
        current === undefined
          ? current
          : { ...current, detail: applyCheckinDelta(current.detail, studentId, checked) },
      )
      return { previous }
    },
    onError: (_error, _variables, context) => {
      queryClient.setQueryData(queryKey, context?.previous)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey }),
  })
}
