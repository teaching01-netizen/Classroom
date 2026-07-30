import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { CourseId } from '@/features/courses'
import { attendanceKeys } from './attendance.keys'
import { attendanceReportSchema, batchAttendanceSchema } from './attendance.schemas'

export function useAttendanceQuery(courseId: CourseId, threshold = 0) {
  return useQuery({
    queryKey: attendanceKeys.detail(courseId, threshold),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.attendance(courseId, threshold), {
        schema: attendanceReportSchema,
        signal,
        timeoutMs: 65_000,
      }),
  })
}

export function useBatchAttendanceQuery(
  courseIds: readonly CourseId[],
  threshold = 0,
) {
  return useQuery({
    queryKey: attendanceKeys.batch(courseIds, threshold),
    queryFn: ({ signal }) =>
      apiClient.post(endpoints.batchAttendance, {
        body: { course_ids: courseIds, threshold },
        schema: batchAttendanceSchema,
        signal,
        timeoutMs: 65_000,
      }),
    enabled: courseIds.length > 0,
  })
}
