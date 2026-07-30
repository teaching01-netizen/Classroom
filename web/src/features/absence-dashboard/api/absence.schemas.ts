import { z } from 'zod'
import { courseIdSchema } from '@/features/courses'
import { sessionIdSchema } from '@/features/sessions'
import { studentIdSchema } from '@/features/checkins'

export const dashboardSortSchema = z.enum(['risk', 'rate-asc', 'rate-desc', 'name'])
export type DashboardSort = z.infer<typeof dashboardSortSchema>

export const dashboardFiltersSchema = z.object({
  courseIds: z.array(courseIdSchema),
  dateRange: z.object({ from: z.string(), to: z.string() }).nullable(),
  threshold: z.number().int().nonnegative(),
  sortBy: dashboardSortSchema,
  wCodes: z.array(z.string()).default([]),
})

export type DashboardFilters = z.infer<typeof dashboardFiltersSchema>

const sessionCheckinSchema = z.object({
  sessionId: sessionIdSchema,
  sessionNumber: z.number().int(),
  sessionName: z.string(),
  sessionDate: z.string(),
  sessionStatus: z.string(),
  checkedIn: z.boolean(),
  status: z.string(),
})

const studentAbsenceSchema = z.object({
  studentId: studentIdSchema,
  name: z.string(),
  nickname: z.string().default(''),
  school: z.string().default(''),
  avatarUrl: z.string().default(''),
  attendedSessions: z.number().int().nonnegative(),
  totalSessions: z.number().int().nonnegative(),
  attendanceRate: z.number().min(0).max(1),
  atRisk: z.boolean(),
  courses: z.array(z.object({
    courseId: courseIdSchema,
    courseName: z.string(),
    totalSessions: z.number().int().nonnegative(),
    attendedSessions: z.number().int().nonnegative(),
    rate: z.number().min(0).max(1),
    absences: z.number().int().nonnegative(),
    atRisk: z.boolean(),
  })),
  perSession: z.array(sessionCheckinSchema),
})

const dashboardSessionSchema = z.object({
  sessionId: sessionIdSchema,
  sessionNumber: z.number().int(),
  name: z.string(),
  date: z.string(),
  courseId: courseIdSchema,
  courseName: z.string(),
  checkedInCount: z.number().int().nonnegative(),
  totalStudents: z.number().int().nonnegative(),
  status: z.string(),
})

export const absenceDashboardSchema = z.object({
  generatedAt: z.string(),
  totalStudents: z.number().int().nonnegative(),
  totalCourses: z.number().int().nonnegative(),
  avgAttendanceRate: z.number().min(0).max(1),
  atRiskCount: z.number().int().nonnegative(),
  topAtRisk: z.array(z.unknown()),
  students: z.array(studentAbsenceSchema),
  sessions: z.array(dashboardSessionSchema),
})

export type AbsenceDashboard = z.infer<typeof absenceDashboardSchema>
export type StudentAbsence = z.infer<typeof studentAbsenceSchema>

export const dashboardViewSchema = z.object({
  id: z.number().int().positive(),
  name: z.string(),
  filters: dashboardFiltersSchema,
  lastUsedAt: z.string(),
  createdAt: z.string(),
  updatedAt: z.string(),
})

export const dashboardViewsSchema = z.array(dashboardViewSchema)
export type DashboardView = z.infer<typeof dashboardViewSchema>
