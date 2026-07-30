import { z } from 'zod'

export const courseIdSchema = z.string().min(1).brand<'CourseId'>()
export type CourseId = z.infer<typeof courseIdSchema>

export const courseStatusSchema = z.enum(['active', 'upcoming', 'finished'])

export const courseSummarySchema = z.object({
  course_id: courseIdSchema,
  name: z.string(),
  start_date: z.string(),
  end_date: z.string(),
  enrolled_count: z.number().int().nonnegative(),
  total_sessions: z.number().int().nonnegative(),
  completed_sessions: z.number().int().nonnegative(),
  avg_attendance_rate: z.number().min(0).max(1),
  status: courseStatusSchema,
})

export const courseListSchema = z.object({
  courses: z.array(courseSummarySchema),
})

export type CourseSummary = z.infer<typeof courseSummarySchema>
