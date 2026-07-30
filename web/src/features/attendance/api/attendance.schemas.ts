import { z } from 'zod'
import { courseIdSchema } from '@/features/courses'
import { sessionIdSchema, sessionStatusSchema } from '@/features/sessions'
import { studentIdSchema } from '@/features/checkins'

export const sessionCellSchema = z.object({
  sessionId: sessionIdSchema,
  sessionNumber: z.number().int(),
  sessionName: z.string(),
  sessionDate: z.string(),
  sessionStatus: sessionStatusSchema,
  checkedIn: z.boolean(),
  status: z.enum(['ok', 'error', 'empty']),
})

export const studentAttendanceSchema = z.object({
  studentId: studentIdSchema,
  name: z.string(),
  nickname: z.string().default(''),
  avatarUrl: z.string().default(''),
  school: z.string().default(''),
  attendedSessions: z.number().int().nonnegative(),
  totalSessions: z.number().int().nonnegative(),
  attendanceRate: z.number().min(0).max(1),
  atRisk: z.boolean(),
  perSession: z.array(sessionCellSchema),
})

const attendanceSessionSchema = z.object({
  session_id: sessionIdSchema,
  session_number: z.number().int(),
  name: z.string(),
  date: z.string(),
  checked_in_count: z.number().int().nonnegative(),
  total_students: z.number().int().nonnegative(),
  status: sessionStatusSchema,
})

const reportErrorSchema = z.object({
  sessionId: sessionIdSchema,
  reason: z.string(),
})

export const attendanceReportSchema = z.object({
  courseId: courseIdSchema,
  courseName: z.string(),
  sessions: z.array(attendanceSessionSchema),
  students: z.array(studentAttendanceSchema),
  errors: z.array(reportErrorSchema),
  truncated: z.boolean(),
  stale: z.boolean().default(false),
  threshold: z.number().int().nonnegative(),
  computedAt: z.string(),
  durationMs: z.number().int().nonnegative(),
})

export const batchAttendanceSchema = z.object({
  courses: z.record(z.string(), attendanceReportSchema),
})

export type AttendanceReport = z.infer<typeof attendanceReportSchema>
