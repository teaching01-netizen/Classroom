import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { useConnectionStore } from '@/shared/realtime/connection-store'
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

export function useCheckinsQuery(courseId: CourseId, sessionId: SessionId) {
  const connected = useConnectionStore((state) => state.status === 'connected')
  return useQuery({
    queryKey: sessionKeys.detail(courseId, sessionId),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.session(courseId, sessionId), {
        schema: sessionDetailSchema,
        signal,
      }),
    refetchInterval: connected ? false : 10_000,
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
      const previous = queryClient.getQueryData<SessionDetail>(queryKey)
      queryClient.setQueryData<SessionDetail>(queryKey, (current) =>
        current === undefined
          ? current
          : {
              ...current,
              students: current.students.map((student) =>
                student.student_id === studentId
                  ? { ...student, checked_in: checked }
                  : student,
              ),
            },
      )
      return { previous }
    },
    onError: (_error, _variables, context) => {
      queryClient.setQueryData(queryKey, context?.previous)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey }),
  })
}
