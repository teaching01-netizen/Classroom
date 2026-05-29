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
    const totalStudents = sessions.length > 0 ? sessions[0].total_students : 0;
    const totalChecked = sessions.reduce((sum, s) => sum + s.checked_in_count, 0);
    const avgAttendance = totalSessions > 0 && totalStudents > 0
      ? Math.round((totalChecked / (totalSessions * totalStudents)) * 100)
      : 0;

    return [
      { value: totalSessions, label: 'Total Sessions' },
      { value: doneSessions, label: 'Completed' },
      { value: totalStudents, label: 'Students' },
      { value: `${avgAttendance}%`, label: 'Attendance' },
    ];
  }, [sessions]);

  if (isLoading) {
    return (
      <div style={{ padding: 'var(--space-xl, 32px)', color: 'var(--text-secondary, #94a3b8)' }}>
        Loading sessions...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 'var(--space-xl, 32px)', color: 'var(--color-danger, #ef4444)' }}>
        Error: {error}
      </div>
    );
  }

  const courseName = sessions.length > 0 ? sessions[0].name : 'Course';

  return (
    <div style={{ padding: 'var(--space-xl, 32px)' }}>
      {isRefreshing && (
        <div style={{
          position: 'fixed',
          top: '12px',
          right: '12px',
          background: 'var(--bg-card, #16213e)',
          border: '1px solid var(--border-default, #2d3a5a)',
          borderRadius: 'var(--radius-md, 8px)',
          padding: '6px 12px',
          fontSize: '12px',
          color: 'var(--text-secondary, #94a3b8)',
          zIndex: 1000,
          opacity: 0.8,
        }}>
          Syncing...
        </div>
      )}
      <BackBreadcrumb to="/" label="Back to Dashboard" />

      <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--text-primary, #fff)', marginBottom: 'var(--space-lg, 24px)' }}>
        {courseName}
      </h2>

      <StatsBar stats={stats} />

      <SessionTable sessions={sessions} courseId={courseId} />

      {sessions.length === 0 && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--text-secondary, #94a3b8)' }}>
          <p style={{ fontSize: '1.25rem' }}>No attendance sessions for this course</p>
        </div>
      )}
    </div>
  );
}
