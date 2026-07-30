import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import type { CourseId } from '@/features/courses'
import { courseDetailSchema } from './session.schemas'
import { sessionKeys } from './session.keys'

export function useCourseSessionsQuery(courseId: CourseId) {
  const connected = useConnectionStore((state) => state.status === 'connected')
  return useQuery({
    queryKey: sessionKeys.course(courseId),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.course(courseId), {
        schema: courseDetailSchema,
        signal,
      }),
    refetchInterval: connected ? false : 10_000,
  })
}
