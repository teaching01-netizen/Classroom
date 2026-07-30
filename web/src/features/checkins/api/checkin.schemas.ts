import { z } from 'zod'
import { sessionSummarySchema } from '@/features/sessions'

export const studentIdSchema = z.string().min(1).brand<'StudentId'>()
export type StudentId = z.infer<typeof studentIdSchema>

export const studentCheckinSchema = z.object({
  student_id: studentIdSchema,
  name: z.string(),
  nickname: z.string().default(''),
  school: z.string().default(''),
  avatar_url: z.string().default(''),
  checked_in: z.boolean(),
  checked_in_at: z.string().nullable().optional(),
  participation_points: z.number().int().default(0),
})

export const sessionDetailSchema = sessionSummarySchema.extend({
  students: z.array(studentCheckinSchema),
  qr_active: z.boolean().default(false),
  qr_expires_at: z.string().nullable().optional(),
  qr_url: z.string().optional(),
})

export const toggleCheckinSchema = z.object({
  student_id: studentIdSchema,
  checked_in: z.boolean(),
  new_count: z.number().int().nonnegative(),
  snapshot_refresh_pending: z.boolean().default(false),
})

export type StudentCheckin = z.infer<typeof studentCheckinSchema>
export type SessionDetail = z.infer<typeof sessionDetailSchema>
