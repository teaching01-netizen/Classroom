import type { Page } from '@playwright/test'
import { xlsxFixtureBase64 as attendanceExportFixtureBase64 } from './attendance-export-fixture'

const course = {
  course_id: 'CS101',
  name: 'Software Engineering',
  start_date: '2026-01-10',
  end_date: '2026-04-10',
  enrolled_count: 2,
  total_sessions: 1,
  completed_sessions: 0,
  avg_attendance_rate: 0.75,
  status: 'active',
}

const newCourse = {
  course_id: 'CS202',
  name: 'Distributed Systems',
  start_date: '2026-01-10',
  end_date: '2026-04-10',
  enrolled_count: 3,
  total_sessions: 2,
  completed_sessions: 1,
  avg_attendance_rate: 0.8,
  status: 'active',
}

const session = {
  session_id: 'S1',
  session_number: 1,
  name: 'Architecture workshop',
  date: '2026-02-01',
  checked_in_count: 1,
  total_students: 2,
  status: 'active',
}

const students = [
  {
    student_id: 'u123',
    name: 'Alice Chen',
    nickname: 'Alice',
    school: 'Computer Science',
    avatar_url: '',
    checked_in: true,
    checked_in_at: null,
    participation_points: 4,
  },
  {
    student_id: 'u456',
    name: 'Sam Rivera',
    nickname: '',
    school: 'Computer Science',
    avatar_url: '',
    checked_in: false,
    checked_in_at: null,
    participation_points: 1,
  },
]

const attendanceReport = {
  courseId: 'CS101',
  courseName: 'Software Engineering',
  sessions: [],
  students: [],
  errors: [],
  truncated: false,
  stale: false,
  threshold: 0,
  computedAt: '2026-02-01T10:00:00Z',
  durationMs: 200,
}

function envelope(data: unknown) {
  return { success: true, data }
}

export type AttendanceExportMode = 'success' | 'failure' | 'delay'

export type MockBackendOptions = {
  readonly exportMode?: AttendanceExportMode
  readonly exportDelayMs?: number
}

const exportedAt = '2026-02-01T10:05:00Z'
const csvHeader = [
  'course_id', 'course_name', 'exported_at', 'source_validated_at',
  'student_id', 'student_name', 'nickname', 'school',
  'attended_sessions', 'total_sessions', 'attendance_rate', 'at_risk', 'Session 1',
].join(',')

function attendanceCsv(present: boolean): string {
  const rows = [
    ['CS101', 'Software Engineering', exportedAt, exportedAt, 'u123', 'Alice Chen', 'Alice', 'Computer Science', '1', '1', '100.00%', 'false', present ? 'Present' : 'Absent'].join(','),
    ['CS101', 'Software Engineering', exportedAt, exportedAt, 'u456', 'Sam Rivera', '', 'Computer Science', '0', '1', '0.00%', 'false', 'Absent'].join(','),
  ]
  return '\ufeff' + [csvHeader, ...rows].join('\n')
}

// A real attendance workbook produced by the Go serializer (excelize): two
// sheets (Attendance, Metadata), frozen header pane, autofilter, percentage
// NumFmt, and shared strings containing the student and course names.
const xlsxFixtureBase64 = attendanceExportFixtureBase64

export async function mockBackend(page: Page, options: MockBackendOptions = {}): Promise<void> {
  // The attendance report is stateful so tests can simulate a scraper refresh:
  // an export request validates freshly scraped data, which marks attendance
  // present for subsequent report reads.
  const state = { attendancePresent: false, catalogSynced: false }
  await page.routeWebSocket('**/ws', (socket) => {
    socket.onMessage(() => undefined)
  })
  await page.route('**/api/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (!path.startsWith('/api/')) {
      await route.continue()
      return
    }

    if (path === '/api/teacher/courses/CS101/attendance-report/export' && request.method() === 'POST') {
      const body = request.postDataJSON()
      if (options.exportMode === 'failure') {
        await route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: JSON.stringify({ success: false, error: 'Latest attendance data could not be validated. Please try again.' }),
        })
        return
      }
      if (options.exportDelayMs !== undefined && options.exportDelayMs > 0) {
        await new Promise((resolve) => setTimeout(resolve, options.exportDelayMs))
      }
      // Simulate the snapshot scraper refreshing the course before serving the
      // export: the fresh data marks the stored attendance present.
      state.attendancePresent = true
      if (body.format === 'xlsx') {
        await route.fulfill({
          status: 200,
          contentType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
          headers: { 'Content-Disposition': 'attachment; filename="attendance-software-engineering-2026-02-01.xlsx"' },
          body: Buffer.from(xlsxFixtureBase64, 'base64'),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'text/csv; charset=utf-8',
        headers: { 'Content-Disposition': 'attachment; filename="attendance-software-engineering-2026-02-01.csv"' },
        body: Buffer.from(attendanceCsv(state.attendancePresent), 'utf8'),
      })
      return
    }

    if (path === '/api/teacher/refresh' && request.method() === 'POST') {
      state.catalogSynced = true
      await route.fulfill({
        json: envelope({
          courses_discovered: 2,
          courses_refreshed: 2,
          sessions_discovered: 2,
          sessions_refreshed: 2,
          profiles_refreshed: true,
          failed_targets: 0,
        }),
      })
      return
    }

    if (path === '/api/teacher/courses' && request.method() === 'GET') {
      await route.fulfill({
        json: envelope({ courses: state.catalogSynced ? [course, newCourse] : [course] }),
      })
      return
    }
    if (path === '/api/teacher/favourites') {
      await route.fulfill({
        json: request.method() === 'GET'
          ? envelope({ favourite_ids: ['CS101'] })
          : envelope(null),
      })
      return
    }
    if (path === '/api/teacher/courses/attendance-batch') {
      await route.fulfill({ json: envelope({ courses: { CS101: attendanceReport } }) })
      return
    }
    if (path === '/api/teacher/courses/CS101/attendance-report') {
      await route.fulfill({
        json: envelope({
          ...attendanceReport,
          sessions: [session],
          students: [{
            studentId: 'u123',
            name: 'Alice Chen',
            nickname: 'Alice',
            avatarUrl: '',
            school: 'Computer Science',
            attendedSessions: 1,
            totalSessions: 1,
            attendanceRate: 1,
            atRisk: false,
            perSession: [{
              sessionId: 'S1',
              sessionNumber: 1,
              sessionName: 'Architecture workshop',
              sessionDate: '2026-02-01',
              sessionStatus: 'done',
              checkedIn: state.attendancePresent,
              status: 'ok',
            }],
          }],
        }),
      })
      return
    }
    if (path === '/api/teacher/courses/CS101' && request.method() === 'GET') {
      await route.fulfill({ json: envelope({ ...course, sessions: [session] }) })
      return
    }
    if (path === '/api/teacher/courses/CS101/sessions/S1' && request.method() === 'GET') {
      await route.fulfill({
        json: envelope({
          ...session,
          students,
          qr_active: true,
          qr_expires_at: '2026-02-01T11:00:00Z',
        }),
      })
      return
    }
    if (path.endsWith('/toggle-checkin')) {
      const body = request.postDataJSON()
      await route.fulfill({
        json: envelope({
          student_id: body.student_id,
          checked_in: body.checked,
          new_count: body.checked ? 2 : 1,
          snapshot_refresh_pending: false,
        }),
      })
      return
    }
    if (path === '/api/rooms' && request.method() === 'GET') {
      await route.fulfill({ json: envelope([]) })
      return
    }
    if (path === '/api/rooms/from-session') {
      await route.fulfill({
        json: envelope({
          room_id: 'S1',
          class_id: 'S1',
          name: 'Architecture workshop',
          status: 'Ready',
        }),
      })
      return
    }
    if (path === '/api/rooms/S1/start') {
      await route.fulfill({ json: envelope(null) })
      return
    }
    if (path === '/api/rooms/S1') {
      await route.fulfill({
        json: envelope({
          room_id: 'S1',
          class_id: 'S1',
          name: 'Architecture workshop',
          status: 'Running',
          qr_url: 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 21 21"%3E%3Crect width="21" height="21" fill="white"/%3E%3Cg fill="black"%3E%3Cpath d="M1 1h7v7H1zM13 1h7v7h-7zM1 13h7v7H1z"/%3E%3Cpath fill="white" d="M3 3h3v3H3zM15 3h3v3h-3zM3 15h3v3H3z"/%3E%3Cpath d="M10 2h1v2h1v2h-2zM9 8h3v1h2v2h-1v2h-2v-2H9zM15 9h2v1h2v2h-3v2h-2v-3h1zM9 14h2v2h2v1h-2v3H9zM14 15h2v1h1v-2h2v3h1v3h-3v-1h-2v1h-2v-2h1z"/%3E%3C/g%3E%3C/svg%3E',
          expires_at: '2026-02-01T11:00:00Z',
        }),
      })
      return
    }
    if (path === '/api/teacher/dashboard-views') {
      await route.fulfill({ json: envelope([]) })
      return
    }
    if (path === '/api/teacher/absence-dashboard') {
      await route.fulfill({
        json: envelope({
          generatedAt: '2026-02-01T10:00:00Z',
          totalStudents: 1,
          totalCourses: 1,
          avgAttendanceRate: 1,
          atRiskCount: 0,
          topAtRisk: [],
          students: [{
            studentId: 'u123',
            name: 'Alice Chen',
            nickname: 'Alice',
            school: 'Computer Science',
            avatarUrl: '',
            attendedSessions: 1,
            totalSessions: 1,
            attendanceRate: 1,
            atRisk: false,
            courses: [{
              courseId: 'CS101',
              courseName: 'Software Engineering',
              totalSessions: 1,
              attendedSessions: 1,
              rate: 1,
              absences: 0,
              atRisk: false,
            }],
            perSession: [{
              sessionId: 'S1',
              sessionNumber: 1,
              sessionName: 'Architecture workshop',
              sessionDate: '2026-02-01',
              sessionStatus: 'done',
              checkedIn: true,
              status: 'ok',
            }],
          }],
          sessions: [{
            sessionId: 'S1',
            sessionNumber: 1,
            name: 'Architecture workshop',
            date: '2026-02-01',
            courseId: 'CS101',
            courseName: 'Software Engineering',
            checkedInCount: 1,
            totalStudents: 2,
            status: 'done',
          }],
        }),
      })
      return
    }
    await route.fulfill({ status: 404, json: { success: false, error: `Unhandled ${path}` } })
  })
}
