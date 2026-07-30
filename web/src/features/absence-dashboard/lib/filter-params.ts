import type { CourseId } from '@/features/courses'
import {
  dashboardFiltersSchema,
  dashboardSortSchema,
  type DashboardFilters,
} from '../api/absence.schemas'

export const defaultDashboardFilters: DashboardFilters = {
  courseIds: [],
  dateRange: null,
  threshold: 0,
  sortBy: 'risk',
  wCodes: [],
}

export function filtersFromSearchParams(params: URLSearchParams): DashboardFilters {
  const courses = params.get('courses')
  const wCodes = params.get('wCodes')
  const parsedSort = dashboardSortSchema.safeParse(params.get('sort'))
  const rawThreshold = Number(params.get('threshold') ?? 0)
  return dashboardFiltersSchema.parse({
    courseIds: courses === null || courses === '' ? [] : courses.split(','),
    dateRange:
      params.get('from') !== null && params.get('to') !== null
        ? { from: params.get('from'), to: params.get('to') }
        : null,
    threshold: Number.isFinite(rawThreshold) && rawThreshold >= 0 ? rawThreshold : 0,
    sortBy: parsedSort.success ? parsedSort.data : 'risk',
    wCodes: wCodes === null || wCodes === '' ? [] : wCodes.split(','),
  })
}

export function filtersToSearchParams(filters: DashboardFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.courseIds.length > 0) {
    params.set('courses', filters.courseIds.join(','))
  }
  if (filters.dateRange !== null) {
    params.set('from', filters.dateRange.from)
    params.set('to', filters.dateRange.to)
  }
  if (filters.threshold > 0) {
    params.set('threshold', String(filters.threshold))
  }
  if (filters.sortBy !== 'risk') {
    params.set('sort', filters.sortBy)
  }
  if (filters.wCodes.length > 0) {
    params.set('wCodes', filters.wCodes.join(','))
  }
  return params
}

export function toggleCourseFilter(
  filters: DashboardFilters,
  courseId: CourseId,
): DashboardFilters {
  return {
    ...filters,
    courseIds: filters.courseIds.includes(courseId)
      ? filters.courseIds.filter((id) => id !== courseId)
      : [...filters.courseIds, courseId],
  }
}
