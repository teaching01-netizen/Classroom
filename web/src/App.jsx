import React, { useEffect } from 'react';
import { BrowserRouter, Routes, Route, Link, useNavigate } from 'react-router-dom';
import { usePinnedCoursesStore } from './store/usePinnedCoursesStore';
import { useCourses } from './hooks/useCourses';
import { useWebSocket } from './hooks/useWebSocket';
import { ErrorBoundary } from './components/ErrorBoundary';
import { CourseDashboard } from './pages/CourseDashboard';
import { SessionList } from './pages/SessionList';
import { CheckinDetail } from './pages/CheckinDetail';

import './styles/tokens.css';

function PinnedCourseCard({ course }) {
  const navigate = useNavigate();
  const unpinCourse = usePinnedCoursesStore((state) => state.unpinCourse);

  const handleUnpin = (e) => {
    e.stopPropagation();
    unpinCourse(course.course_id);
  };

  return (
    <div
      onClick={() => navigate(`/courses/${course.course_id}/sessions`)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          navigate(`/courses/${course.course_id}/sessions`);
        }
      }}
      role="button"
      tabIndex={0}
      style={{
        background: 'var(--color-bg, #FFFFFF)',
        borderRadius: 'var(--radius-xl, 12px)',
        padding: 'var(--space-6, 24px)',
        border: '1px solid var(--color-border, #DCDBDD)',
        cursor: 'pointer',
        transition: 'border-color 0.2s, box-shadow 0.2s',
        position: 'relative',
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.borderColor = 'var(--color-border-strong, #CFCFD9)';
        e.currentTarget.style.boxShadow = 'var(--shadow-md, 0 8px 24px rgba(16, 24, 40, 0.10))';
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = 'var(--color-border, #DCDBDD)';
        e.currentTarget.style.boxShadow = 'none';
      }}
    >
      <button
        onClick={handleUnpin}
        aria-label={`Unpin ${course.name || course.course_id}`}
        style={{
          position: 'absolute',
          top: 'var(--space-4, 16px)',
          right: 'var(--space-4, 16px)',
          background: 'transparent',
          border: 'none',
          color: 'var(--color-text-muted, #696A6C)',
          cursor: 'pointer',
          fontSize: '1.25rem',
          lineHeight: 1,
          padding: '4px 8px',
          borderRadius: 'var(--radius-sm, 6px)',
          transition: 'color 0.2s',
        }}
        onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--color-danger, #9A3D4A)'; }}
        onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--color-text-muted, #696A6C)'; }}
      >
        ✕
      </button>

      <div style={{ marginBottom: 'var(--space-4, 16px)' }}>
        <h3 style={{ fontSize: '1.125rem', fontWeight: '600', marginBottom: 'var(--space-1, 4px)', paddingRight: '32px' }}>
          {course.name || course.course_id}
        </h3>
        <p style={{ fontSize: '0.875rem', color: 'var(--color-text-secondary, #4F5056)' }}>
          {course.course_id}
        </p>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2, 8px)', marginBottom: 'var(--space-4, 16px)', fontSize: '0.875rem', color: 'var(--color-text-secondary, #4F5056)' }}>
        {course.status && (
          <>
            <span style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              background: course.status === 'active' ? 'var(--color-success, #257348)' : 'var(--color-text-disabled, #B8BCC4)',
              display: 'inline-block',
            }} />
            <span>{course.status}</span>
          </>
        )}
        {course.term_dates && (
          <span style={{ marginLeft: 'auto' }}>{course.term_dates}</span>
        )}
      </div>

      <div style={{ display: 'flex', gap: 'var(--space-6, 24px)', fontSize: '0.875rem', color: 'var(--color-text-secondary, #4F5056)' }}>
        {course.enrolled_count != null && (
          <span>{course.enrolled_count} enrolled</span>
        )}
        {course.total_sessions != null && (
          <span>{course.total_sessions} sessions</span>
        )}
      </div>

      {course.avg_attendance_rate != null && (() => {
        const attendancePercent = Math.min(Math.max(Math.round(course.avg_attendance_rate * 100), 0), 100);
        return (
          <div style={{ marginTop: 'var(--space-4, 16px)' }}>
            <div style={{
              height: '4px',
              borderRadius: '2px',
              background: 'var(--color-bg-hover, #F1F2F4)',
              overflow: 'hidden',
            }}>
              <div style={{
                height: '100%',
                width: `${attendancePercent}%`,
                borderRadius: '2px',
                background: attendancePercent >= 70 ? 'var(--color-success, #257348)' : attendancePercent >= 40 ? 'var(--color-warning, #7A631C)' : 'var(--color-danger, #9A3D4A)',
              }} />
            </div>
          </div>
        );
      })()}
    </div>
  );
}

function HomePage() {
  const { courses, isLoading, error } = useCourses();
  const pinnedCourseIds = usePinnedCoursesStore((state) => state.pinnedCourseIds);
  const loadFavourites = usePinnedCoursesStore((state) => state.loadFavourites);

  useWebSocket();

  useEffect(() => {
    loadFavourites();
  }, [loadFavourites]);

  const pinnedCourses = courses.filter((c) => pinnedCourseIds.includes(c.course_id));

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
          <PinnedCourseCard key={course.course_id} course={course} />
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
            <Route path="/courses/:courseId/sessions/:sessionId" element={<CheckinDetail />} />
          </Routes>
        </ErrorBoundary>
      </div>
    </BrowserRouter>
  );
}

export default App;
