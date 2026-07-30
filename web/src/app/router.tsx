import { createBrowserRouter } from 'react-router-dom'
import { AppShell, AppShellFrame } from './AppShell'
import { RootRouteErrorBoundary } from './route-error-boundary'
import { NotFoundRoute } from './not-found-route'
import { Skeleton } from '@/shared/ui/Skeleton'

export const router = createBrowserRouter([
  {
    path: '/',
    Component: AppShell,
    ErrorBoundary: RootRouteErrorBoundary,
    HydrateFallback: RouteHydrateFallback,
    children: [
      {
        index: true,
        lazy: () => import('@/features/courses/routes/HomeRoute'),
      },
      {
        path: 'courses',
        lazy: () => import('@/features/courses/routes/CoursesRoute'),
      },
      {
        path: 'courses/:courseId/sessions',
        lazy: () => import('@/features/sessions/routes/SessionsRoute'),
      },
      {
        path: 'courses/:courseId/sessions/:sessionId',
        lazy: () => import('@/features/checkins/routes/CheckinRoute'),
      },
      {
        path: 'courses/:courseId/attendance',
        lazy: () => import('@/features/attendance/routes/AttendanceRoute'),
      },
      {
        path: 'absence-dashboard',
        lazy: () => import('@/features/absence-dashboard/routes/AbsenceDashboardRoute'),
      },
      {
        path: '*',
        Component: NotFoundRoute,
      },
    ],
  },
])

function RouteHydrateFallback() {
  return (
    <AppShellFrame>
      <div className="route-loading">
        <Skeleton lines={5} />
      </div>
    </AppShellFrame>
  )
}
