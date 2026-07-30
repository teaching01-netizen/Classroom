import type { CourseId } from '@/features/courses'
import type { SessionId } from './session.schemas'

export const sessionKeys = {
  all: ['sessions'] as const,
  course: (courseId: CourseId) => [...sessionKeys.all, courseId] as const,
  detail: (courseId: CourseId, sessionId: SessionId) =>
    [...sessionKeys.course(courseId), sessionId] as const,
} as const
