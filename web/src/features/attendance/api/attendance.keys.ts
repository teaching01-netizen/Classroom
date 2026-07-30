import type { CourseId } from '@/features/courses'

export const attendanceKeys = {
  all: ['attendance'] as const,
  detail: (courseId: CourseId, threshold: number) =>
    [...attendanceKeys.all, courseId, threshold] as const,
  batch: (courseIds: readonly CourseId[], threshold: number) =>
    [...attendanceKeys.all, 'batch', [...courseIds].sort(), threshold] as const,
} as const
