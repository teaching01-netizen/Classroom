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
      style={{
        background: 'var(--bg-card, #16213e)',
        borderRadius: 'var(--radius-lg, 12px)',
        padding: 'var(--space-lg, 24px)',
        border: '1px solid var(--border-default, #2d3a5a)',
        cursor: 'pointer',
        transition: 'border-color 0.2s, box-shadow 0.2s',
        position: 'relative',
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.borderColor = 'var(--border-hover, #3d4a6a)';
        e.currentTarget.style.boxShadow = 'var(--shadow-card-hover, 0 8px 24px rgba(0,0,0,0.4))';
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = 'var(--border-default, #2d3a5a)';
        e.currentTarget.style.boxShadow = 'none';
      }}
    >
      <button
        onClick={handleUnpin}
        aria-label={`Unpin ${course.name || course.course_id}`}
        style={{
          position: 'absolute',
          top: 'var(--space-md, 16px)',
          right: 'var(--space-md, 16px)',
          background: 'transparent',
          border: 'none',
          color: 'var(--text-muted, #64748b)',
          cursor: 'pointer',
          fontSize: '1.25rem',
          lineHeight: 1,
          padding: '4px 8px',
          borderRadius: 'var(--radius-sm, 6px)',
          transition: 'color 0.2s',
        }}
        onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--color-danger, #ef4444)'; }}
        onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-muted, #64748b)'; }}
      >
        ✕
      </button>

      <div style={{ marginBottom: 'var(--space-md, 16px)' }}>
        <h3 style={{ fontSize: '1.125rem', fontWeight: '600', marginBottom: 'var(--space-xs, 4px)', paddingRight: '32px' }}>
          {course.name || course.course_id}
        </h3>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary, #94a3b8)' }}>
          {course.course_id}
        </p>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-sm, 8px)', marginBottom: 'var(--space-md, 16px)', fontSize: '0.875rem', color: 'var(--text-secondary, #94a3b8)' }}>
        {course.status && (
          <>
            <span style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              background: course.status === 'active' ? 'var(--color-success, #4ade80)' : 'var(--color-inactive, #94a3b8)',
              display: 'inline-block',
            }} />
            <span>{course.status}</span>
          </>
        )}
        {course.term_dates && (
          <span style={{ marginLeft: 'auto' }}>{course.term_dates}</span>
        )}
      </div>

      <div style={{ display: 'flex', gap: 'var(--space-lg, 24px)', fontSize: '0.875rem', color: 'var(--text-secondary, #94a3b8)' }}>
        {course.enrolled_count != null && (
          <span>{course.enrolled_count} enrolled</span>
        )}
        {course.sessions_count != null && (
          <span>{course.sessions_count} sessions</span>
        )}
      </div>

      {course.attendance_rate != null && (
        <div style={{ marginTop: 'var(--space-md, 16px)' }}>
          <div style={{
            height: '4px',
            borderRadius: '2px',
            background: 'var(--bg-input, #1a1a2e)',
            overflow: 'hidden',
          }}>
            <div style={{
              height: '100%',
              width: `${Math.round(course.attendance_rate * 100)}%`,
              borderRadius: '2px',
              background: course.attendance_rate >= 0.7 ? 'var(--color-success, #4ade80)' : course.attendance_rate >= 0.4 ? 'var(--color-warning, #fbbf24)' : 'var(--color-danger, #ef4444)',
            }} />
          </div>
        </div>
      )}
    </div>
  );
}

function HomePage() {
  const { courses, isLoading } = useCourses();
  const pinnedCourseIds = usePinnedCoursesStore((state) => state.pinnedCourseIds);
  const cleanupStalePins = usePinnedCoursesStore((state) => state.cleanupStalePins);

  useWebSocket();

  useEffect(() => {
    if (courses.length > 0) {
      cleanupStalePins(courses.map((c) => c.course_id));
    }
  }, [courses, cleanupStalePins]);

  const pinnedCourses = courses.filter((c) => pinnedCourseIds.includes(c.course_id));

  return (
    <main style={{ padding: 'var(--space-xl, 32px)' }}>
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(min(350px, 100%), 1fr))',
        gap: 'var(--space-lg, 24px)',
      }}>
        {pinnedCourses.map((course) => (
          <PinnedCourseCard key={course.course_id} course={course} />
        ))}
      </div>

      {pinnedCourses.length === 0 && !isLoading && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--text-secondary, #94a3b8)' }}>
          <p style={{ fontSize: '1.25rem', marginBottom: '16px' }}>No pinned courses yet</p>
          <p style={{ marginBottom: '24px' }}>Browse your courses and pin the ones you use regularly</p>
          <Link to="/courses" style={{
            padding: '10px 24px',
            borderRadius: 'var(--radius-md, 8px)',
            background: 'var(--color-accent, #6366f1)',
            color: '#fff',
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
      background: 'var(--bg-card, #16213e)',
      borderBottom: '1px solid var(--border-default, #2d3a5a)',
      padding: '12px var(--space-xl, 32px)',
      display: 'flex',
      gap: 'var(--space-lg, 24px)',
      alignItems: 'center',
    }}>
      <Link to="/" style={{ color: 'var(--text-secondary, #94a3b8)', textDecoration: 'none', fontWeight: '500' }}>
        Dashboard
      </Link>
      <Link to="/courses" style={{ color: 'var(--text-secondary, #94a3b8)', textDecoration: 'none', fontWeight: '500' }}>
        All Courses
      </Link>
    </nav>
  );
}

function App() {
  return (
    <BrowserRouter>
      <div style={{ minHeight: '100vh', background: 'var(--bg-primary, #0f172a)' }}>
        <header style={{
          background: 'var(--bg-card, #16213e)',
          borderBottom: '1px solid var(--border-default, #2d3a5a)',
          padding: '20px var(--space-xl, 32px)',
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
