import React, { useEffect } from 'react';
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom';
import { usePinnedCoursesStore } from './store/usePinnedCoursesStore';
import { useCourses } from './hooks/useCourses';
import { useBatchAttendance } from './hooks/useBatchAttendance';
import { useWebSocket } from './hooks/useWebSocket';
import { ErrorBoundary } from './components/ErrorBoundary';
import { CourseDashboard } from './pages/CourseDashboard';
import { SessionList } from './pages/SessionList';
import { CheckinDetail } from './pages/CheckinDetail';
import { CourseAttendance } from './pages/CourseAttendance';
import { AbsenceDashboard } from './pages/AbsenceDashboard';
import { DashboardCourseCard } from './components/dashboard/DashboardCourseCard';

import './styles/tokens.css';

function HomePage() {
  const { courses, isLoading, error } = useCourses();
  const pinnedCourseIds = usePinnedCoursesStore((state) => state.pinnedCourseIds);
  const loadFavourites = usePinnedCoursesStore((state) => state.loadFavourites);

  useWebSocket();

  useEffect(() => {
    loadFavourites();
  }, [loadFavourites]);

  const pinnedCourses = courses.filter((c) => pinnedCourseIds.includes(c.course_id));
  const pinnedIds = pinnedCourses.map((c) => c.course_id);
  const { data: attendanceData, loading: attendanceLoading, error: attendanceError } = useBatchAttendance(pinnedIds);

  return (
    <main style={{ padding: 'var(--space-8, 32px)' }}>
      {error && (
        <div style={{
          padding: 'var(--space-6, 24px)',
          background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
          color: 'var(--color-danger, #9A3D4A)',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-6, 24px)',
        }}>
          Failed to load courses: {error}
        </div>
      )}

      {isLoading && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--color-text-secondary, #4F5056)' }}>
          Loading courses...
        </div>
      )}

      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(min(350px, 100%), 1fr))',
        gap: 'var(--space-6, 24px)',
      }}>
        {pinnedCourses.map((course) => (
          <DashboardCourseCard
            key={course.course_id}
            course={course}
            attendanceData={attendanceData?.[course.course_id] ?? null}
            attendanceLoading={attendanceLoading}
            attendanceError={attendanceError}
          />
        ))}
      </div>

      {!isLoading && !error && pinnedCourses.length === 0 && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--color-text-secondary, #4F5056)' }}>
          <p style={{ fontSize: '1.25rem', marginBottom: '16px' }}>No pinned courses yet</p>
          <p style={{ marginBottom: '24px' }}>Browse your courses and pin the ones you use regularly</p>
          <Link to="/courses" style={{
            padding: '10px 24px',
            borderRadius: 'var(--radius-md, 8px)',
            background: 'var(--color-primary-600, #276BF0)',
            color: 'var(--color-text-inverse, #FFFFFF)',
            textDecoration: 'none',
            fontWeight: '500',
          }}>
            Browse All Courses
          </Link>
        </div>
      )}
    </main>
  );
}

function NavBar() {
  return (
    <nav style={{
      background: 'var(--color-bg, #FFFFFF)',
      borderBottom: '1px solid var(--color-border, #DCDBDD)',
      padding: '12px var(--space-8, 32px)',
      display: 'flex',
      gap: 'var(--space-6, 24px)',
      alignItems: 'center',
    }}>
      <Link to="/" style={{ color: 'var(--color-text-secondary, #4F5056)', textDecoration: 'none', fontWeight: '500' }}>
        Dashboard
      </Link>
      <Link to="/absence-dashboard" style={{ color: 'var(--color-text-secondary, #4F5056)', textDecoration: 'none', fontWeight: '500' }}>
        Absence Dashboard
      </Link>
      <Link to="/courses" style={{ color: 'var(--color-text-secondary, #4F5056)', textDecoration: 'none', fontWeight: '500' }}>
        All Courses
      </Link>
    </nav>
  );
}

function App() {
  return (
    <BrowserRouter>
      <div style={{ minHeight: '100vh', background: 'var(--color-bg-app, #FBFBFB)' }}>
        <header style={{
          background: 'var(--color-bg, #FFFFFF)',
          borderBottom: '1px solid var(--color-border, #DCDBDD)',
          padding: '20px var(--space-8, 32px)',
        }}>
          <h1 style={{ fontSize: '1.75rem', fontWeight: '700' }}>Check-in QR Command Center</h1>
        </header>

        <NavBar />

        <ErrorBoundary>
          <Routes>
            <Route path="/" element={<HomePage />} />
            <Route path="/courses" element={<CourseDashboard />} />
            <Route path="/courses/:courseId/sessions" element={<SessionList />} />
            <Route path="/courses/:courseId/attendance" element={<CourseAttendance />} />
            <Route path="/absence-dashboard" element={<AbsenceDashboard />} />
            <Route path="/courses/:courseId/sessions/:sessionId" element={<CheckinDetail />} />
          </Routes>
        </ErrorBoundary>
      </div>
    </BrowserRouter>
  );
}

export default App;
