import { useQuery } from '@tanstack/react-query'
import { apiClient, type SnapshotVersionInfo } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { CourseId } from '@/features/courses'
import { attendanceKeys } from './attendance.keys'
import { attendanceReportSchema, batchAttendanceSchema, type AttendanceReport } from './attendance.schemas'

// The cached attendance entry: the parsed report plus the server's snapshot
// envelope. Both hooks below share one query key and queryFn, so the report
// and its freshness metadata always come from the same fetch.
type AttendanceQueryResult = {
  readonly detail: AttendanceReport
  readonly snapshot: SnapshotVersionInfo | undefined
}

export function useAttendanceQuery(courseId: CourseId, threshold = 0) {
  return useQuery({
    queryKey: attendanceKeys.detail(courseId, threshold),
    queryFn: ({ signal }) =>
      apiClient
        .get(endpoints.attendance(courseId, threshold), {
          schema: attendanceReportSchema,
          signal,
          timeoutMs: 65_000,
          includeSnapshot: true,
        })
        .then((result): AttendanceQueryResult => ({ detail: result.data, snapshot: result.snapshot })),
    select: (result: AttendanceQueryResult) => result.detail,
  })
}

export function useAttendanceSnapshotQuery(courseId: CourseId, threshold = 0) {
  return useQuery({
    queryKey: attendanceKeys.detail(courseId, threshold),
    queryFn: ({ signal }) =>
      apiClient
        .get(endpoints.attendance(courseId, threshold), {
          schema: attendanceReportSchema,
          signal,
          timeoutMs: 65_000,
          includeSnapshot: true,
        })
        .then((result): AttendanceQueryResult => ({ detail: result.data, snapshot: result.snapshot })),
    select: (result: AttendanceQueryResult) => result.snapshot,
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
