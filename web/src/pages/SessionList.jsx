import React, { useMemo } from 'react';
import { useParams } from 'react-router-dom';
import { useSessions } from '../hooks/useSessions';
import { StatsBar } from '../components/StatsBar';
import { SessionTable } from '../components/SessionTable';
import { BackBreadcrumb } from '../components/BackBreadcrumb';

export function SessionList() {
  const { courseId } = useParams();
  const { sessions, isLoading, isRefreshing, error } = useSessions(courseId);

  const stats = useMemo(() => {
    const totalSessions = sessions.length;
    const doneSessions = sessions.filter(s => s.status === 'done').length;

    // checked_in_count and total_students are not available at session-list level
    // (require individual session detail API calls). Show placeholders.
    const totalChecked = sessions.reduce((sum, s) => sum + s.checked_in_count, 0);

    return [
      { value: totalSessions, label: 'Total Sessions' },
      { value: doneSessions, label: 'Completed' },
      { value: '—', label: 'Students' },
      { value: '—', label: 'Attendance' },
    ];
  }, [sessions]);

  if (isLoading) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)', color: 'var(--color-text-secondary, #4F5056)' }}>
        Loading sessions...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)', color: 'var(--color-danger, #9A3D4A)' }}>
        Error: {error}
      </div>
    );
  }

  const courseName = sessions.length > 0 ? sessions[0].name : 'Course';

  return (
    <div style={{ padding: 'var(--space-8, 32px)' }}>
      {isRefreshing && (
        <div style={{
          position: 'fixed',
          top: '12px',
          right: '12px',
          background: 'var(--color-bg, #FFFFFF)',
          border: '1px solid var(--color-border, #DCDBDD)',
          borderRadius: 'var(--radius-md, 8px)',
          padding: '6px 12px',
          fontSize: '12px',
          color: 'var(--color-text-secondary, #4F5056)',
          zIndex: 1000,
          opacity: 0.8,
        }}>
          Syncing...
        </div>
      )}
      <BackBreadcrumb to="/" label="Back to Dashboard" />

      <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--color-text-primary, #111113)', marginBottom: 'var(--space-6, 24px)' }}>
        {courseName}
      </h2>

      <StatsBar stats={stats} />

      <SessionTable sessions={sessions} courseId={courseId} />

      {sessions.length === 0 && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--color-text-secondary, #4F5056)' }}>
          <p style={{ fontSize: '1.25rem' }}>No attendance sessions for this course</p>
        </div>
      )}
    </div>
  );
}
