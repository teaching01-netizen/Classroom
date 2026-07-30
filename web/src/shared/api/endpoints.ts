const encode = encodeURIComponent

export const endpoints = {
  courses: '/api/teacher/courses',
  course: (courseId: string) => `/api/teacher/courses/${encode(courseId)}`,
  session: (courseId: string, sessionId: string) =>
    `/api/teacher/courses/${encode(courseId)}/sessions/${encode(sessionId)}`,
  toggleCheckin: (courseId: string, sessionId: string) =>
    `/api/teacher/courses/${encode(courseId)}/sessions/${encode(sessionId)}/toggle-checkin`,
  attendance: (courseId: string, threshold: number) =>
    `/api/teacher/courses/${encode(courseId)}/attendance-report?threshold=${threshold}`,
  batchAttendance: '/api/teacher/courses/attendance-batch',
  absenceDashboard: (filters: string) =>
    `/api/teacher/absence-dashboard?filters=${encode(filters)}`,
  favourites: '/api/teacher/favourites',
  favourite: (courseId: string) => `/api/teacher/favourites/${encode(courseId)}`,
  dashboardViews: '/api/teacher/dashboard-views',
  dashboardView: (viewId: number) => `/api/teacher/dashboard-views/${viewId}`,
  touchDashboardView: (viewId: number) => `/api/teacher/dashboard-views/${viewId}/use`,
  rooms: '/api/rooms?lite=true',
  room: (roomId: string) => `/api/rooms/${encode(roomId)}`,
  startRoom: (roomId: string) => `/api/rooms/${encode(roomId)}/start`,
  roomFromSession: '/api/rooms/from-session',
} as const
