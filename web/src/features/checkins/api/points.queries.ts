import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { CourseId } from '@/features/courses'
import { sessionKeys, type SessionId } from '@/features/sessions'
import { givePointsResultSchema, type StudentId } from './checkin.schemas'

type GivePointsVariables = {
  readonly studentId: StudentId
  readonly points: number
  readonly expectedPoints?: number | undefined
}

export function useGivePointsMutation(courseId: CourseId, sessionId: SessionId) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ studentId, points, expectedPoints }: GivePointsVariables) =>
      apiClient.post(endpoints.givePoints(courseId, sessionId, studentId), {
        body: { points, ...(expectedPoints === undefined ? {} : { expectedPoints }) },
        schema: givePointsResultSchema,
      }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: sessionKeys.detail(courseId, sessionId) }),
  })
}
