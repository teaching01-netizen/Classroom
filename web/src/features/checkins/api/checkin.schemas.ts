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
  // Old snapshot payloads predate this field and render no give-points
  // control: eligibility defaults to false (fail closed).
  can_give_point: z.boolean().default(false),
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

export const checkinMutationSchema = z.object({
  status: z.enum(['confirmed', 'already_satisfied', 'pending_verification']),
  checkedIn: z.boolean(),
  snapshotVersion: z.number().int(),
  refreshPending: z.boolean(),
})

export const givePointsResultSchema = z.object({
  studentId: z.string(),
  pointsAwarded: z.number().int().nonnegative(),
  participationPoints: z.number().int().nonnegative(),
  snapshotVersion: z.number().int(),
  snapshotRefreshPending: z.boolean().default(false),
})

export type StudentCheckin = z.infer<typeof studentCheckinSchema>
export type SessionDetail = z.infer<typeof sessionDetailSchema>
