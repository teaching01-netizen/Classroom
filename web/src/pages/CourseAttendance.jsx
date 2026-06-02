import React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useCourseAttendance } from '../hooks/useCourseAttendance';
import { AttendanceTable } from '../components/AttendanceTable';
import { BackBreadcrumb } from '../components/BackBreadcrumb';

export function CourseAttendance() {
  const { courseId } = useParams();
  const { data, loading, error, refetch, truncated, errors } = useCourseAttendance(courseId);

  if (loading) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)' }}>
        <BackBreadcrumb to={`/courses/${courseId}/sessions`} label="Back to Sessions" />
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--color-text-secondary, #4F5056)' }}>
          <div style={{ marginBottom: '16px' }}>
            <div
              style={{
                width: '32px',
                height: '32px',
                border: '3px solid var(--color-border, #DCDBDD)',
                borderTopColor: 'var(--color-primary-600, #276BF0)',
                borderRadius: '50%',
                animation: 'spin 0.8s linear infinite',
                margin: '0 auto',
              }}
            />
          </div>
          <p style={{ fontSize: '1.125rem', fontWeight: '500', marginBottom: '8px' }}>
            Computing attendance report...
          </p>
          <p style={{ fontSize: '0.875rem', color: 'var(--color-text-muted, #696A6C)' }}>
            This may take up to 60 seconds while fetching live session data.
          </p>
          <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)' }}>
        <BackBreadcrumb to={`/courses/${courseId}/sessions`} label="Back to Sessions" />
        <div
          style={{
            padding: 'var(--space-6, 24px)',
            background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
            color: 'var(--color-danger, #9A3D4A)',
            borderRadius: 'var(--radius-md, 8px)',
            marginBottom: 'var(--space-6, 24px)',
          }}
        >
          <p style={{ fontWeight: '500', marginBottom: '8px' }}>Failed to load attendance report</p>
          <p style={{ fontSize: '0.875rem', marginBottom: '16px' }}>{error}</p>
          <button
            onClick={refetch}
            style={{
              padding: '8px 16px',
              borderRadius: 'var(--radius-md, 8px)',
              border: '1px solid var(--color-danger, #9A3D4A)',
              background: 'transparent',
              color: 'var(--color-danger, #9A3D4A)',
              cursor: 'pointer',
              fontWeight: '500',
              fontSize: '0.875rem',
            }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  const atRiskCount = data.students?.filter((s) => s.atRisk).length || 0;

  return (
    <div style={{ padding: 'var(--space-8, 32px)' }}>
      <BackBreadcrumb to={`/courses/${courseId}/sessions`} label="Back to Sessions" />

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 'var(--space-6, 24px)',
          flexWrap: 'wrap',
          gap: 'var(--space-4, 16px)',
        }}
      >
        <div>
          <h2
            style={{
              fontSize: '1.5rem',
              fontWeight: '600',
              color: 'var(--color-text-primary, #111113)',
              marginBottom: 'var(--space-1, 4px)',
            }}
          >
            Attendance Report
          </h2>
          <p style={{ fontSize: '0.875rem', color: 'var(--color-text-secondary, #4F5056)' }}>
            {data.courseName || courseId}
          </p>
        </div>

        <button
          onClick={refetch}
          style={{
            padding: '8px 16px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'var(--color-bg, #FFFFFF)',
            color: 'var(--color-text-secondary, #4F5056)',
            cursor: 'pointer',
            fontWeight: '500',
            fontSize: '0.875rem',
            transition: 'border-color 0.2s',
          }}
          onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--color-border-strong, #CFCFD9)'; }}
          onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--color-border, #DCDBDD)'; }}
        >
          Refresh
        </button>
      </div>

      {/* Summary stats */}
      <div
        style={{
          display: 'flex',
          gap: 'var(--space-6, 24px)',
          marginBottom: 'var(--space-6, 24px)',
          flexWrap: 'wrap',
        }}
      >
        <StatPill label="Students" value={data.students?.length || 0} />
        <StatPill label="Sessions" value={data.sessions?.length || 0} />
        <StatPill
          label="At Risk"
          value={atRiskCount}
          color={atRiskCount > 0 ? 'var(--color-warning, #7A631C)' : undefined}
        />
        {data.durationMs != null && (
          <StatPill label="Computed in" value={`${(data.durationMs / 1000).toFixed(1)}s`} />
        )}
      </div>

      {truncated && (
        <div
          style={{
            padding: 'var(--space-3, 12px) var(--space-4, 16px)',
            background: 'var(--color-warning-bg, #FAF0C4)',
            color: 'var(--color-warning, #7A631C)',
            borderRadius: 'var(--radius-md, 8px)',
            marginBottom: 'var(--space-6, 24px)',
            fontSize: '0.875rem',
          }}
        >
          Report was truncated due to timeout. Some sessions may be missing.
        </div>
      )}

      {errors.length > 0 && (
        <div
          style={{
            padding: 'var(--space-3, 12px) var(--space-4, 16px)',
            background: 'var(--color-danger-bg, #F9E0E3)',
            color: 'var(--color-danger, #9A3D4A)',
            borderRadius: 'var(--radius-md, 8px)',
            marginBottom: 'var(--space-6, 24px)',
            fontSize: '0.875rem',
          }}
        >
          <p style={{ fontWeight: '500', marginBottom: '4px' }}>
            {errors.length} session{errors.length !== 1 ? 's' : ''} failed to load
          </p>
          {errors.map((e) => (
            <div key={e.sessionId} style={{ fontSize: '0.75rem', opacity: 0.8 }}>
              Session {e.sessionId}: {e.reason}
            </div>
          ))}
        </div>
      )}

      <AttendanceTable report={data} />
    </div>
  );
}

function StatPill({ label, value, color }) {
  return (
    <div
      style={{
        padding: 'var(--space-3, 12px) var(--space-4, 16px)',
        background: 'var(--color-bg, #FFFFFF)',
        border: '1px solid var(--color-border, #DCDBDD)',
        borderRadius: 'var(--radius-md, 8px)',
        minWidth: '100px',
      }}
    >
      <div
        style={{
          fontSize: '1.25rem',
          fontWeight: '700',
          color: color || 'var(--color-text-primary, #111113)',
        }}
      >
        {value}
      </div>
      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #696A6C)' }}>
        {label}
      </div>
    </div>
  );
}
