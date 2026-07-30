import type { CourseId } from './course.schemas'

export type CourseFilters = {
  readonly search: string
  readonly status: 'all' | 'active' | 'upcoming' | 'finished'
}

export const courseKeys = {
  all: ['courses'] as const,
  list: (filters?: CourseFilters) =>
    [...courseKeys.all, 'list', filters ?? { search: '', status: 'all' }] as const,
  detail: (courseId: CourseId) => [...courseKeys.all, 'detail', courseId] as const,
  favourites: () => [...courseKeys.all, 'favourites'] as const,
} as const
