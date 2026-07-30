import { z } from 'zod'
import { courseIdSchema, courseSummarySchema } from '@/features/courses'

export const sessionIdSchema = z.string().min(1).brand<'SessionId'>()
export type SessionId = z.infer<typeof sessionIdSchema>

export const sessionStatusSchema = z.enum(['not_started', 'active', 'done', 'auth_error'])

export const sessionSummarySchema = z.object({
  session_id: sessionIdSchema,
  session_number: z.number().int(),
  name: z.string(),
  date: z.string(),
  checked_in_count: z.number().int().nonnegative(),
  total_students: z.number().int().nonnegative(),
  status: sessionStatusSchema,
})

export const courseDetailSchema = courseSummarySchema.extend({
  course_id: courseIdSchema,
  sessions: z.array(sessionSummarySchema),
})

export type SessionSummary = z.infer<typeof sessionSummarySchema>
export type CourseDetail = z.infer<typeof courseDetailSchema>
