import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { usePinnedCoursesStore } from '../../store/usePinnedCoursesStore';
import { AttendanceTable } from '../AttendanceTable';

export function DashboardCourseCard({ course, attendanceData, attendanceLoading, attendanceError }) {
  const navigate = useNavigate();
  const unpinCourse = usePinnedCoursesStore((state) => state.unpinCourse);
  const [expanded, setExpanded] = useState(false);

  const handleUnpin = (e) => {
    e.stopPropagation();
    unpinCourse(course.course_id);
  };

  const data = attendanceData;
  const loading = attendanceLoading ?? false;
  const error = attendanceError ?? null;

  const atRiskStudents = data?.students?.filter((s) => s.atRisk) ?? [];
  const hasDoneSessions = data?.sessions?.length > 0;

  const attendancePercent = course.avg_attendance_rate != null
    ? Math.min(Math.max(Math.round(course.avg_attendance_rate * 100), 0), 100)
    : null;

  return (
    <div
      style={{
        background: 'var(--color-bg, #FFFFFF)',
        borderRadius: 'var(--radius-xl, 12px)',
        padding: 'var(--space-6, 24px)',
        border: '1px solid var(--color-border, #DCDBDD)',
        cursor: expanded ? 'default' : 'pointer',
        transition: 'border-color 0.2s, box-shadow 0.2s',
        position: 'relative',
      }}
      onClick={() => !expanded && navigate(`/courses/${course.course_id}/sessions`)}
      onKeyDown={(e) => {
        if (!expanded && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          navigate(`/courses/${course.course_id}/sessions`);
        }
      }}
      role="button"
      tabIndex={0}
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

      {attendancePercent != null && (
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
      )}

      {!loading && !error && data && hasDoneSessions && atRiskStudents.length > 0 && (
        <div style={{ marginTop: 'var(--space-4, 16px)' }}>
          <div style={{
            padding: 'var(--space-3, 12px) var(--space-4, 16px)',
            background: 'color-mix(in srgb, var(--color-warning, #7A631C) 8%, transparent)',
            borderRadius: 'var(--radius-md, 8px)',
            marginBottom: 'var(--space-3, 12px)',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2, 8px)',
          }}>
            <span style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              background: 'var(--color-warning, #7A631C)',
              display: 'inline-block',
              flexShrink: 0,
            }} />
            <span style={{
              fontSize: '0.8125rem',
              fontWeight: '600',
              color: 'var(--color-warning, #7A631C)',
            }}>
              {atRiskStudents.length} student{atRiskStudents.length !== 1 ? 's' : ''} at risk
            </span>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-2, 8px)' }}>
            {atRiskStudents.map((student) => {
              const ratePercent = Math.round(student.attendanceRate * 100);
              const absences = student.totalSessions - student.attendedSessions;
              return (
                <div
                  key={student.studentId}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--space-3, 12px)',
                    padding: 'var(--space-2, 8px) var(--space-3, 12px)',
                    background: 'var(--color-bg, #FFFFFF)',
                    border: '1px solid var(--color-border-subtle, #EEEFF1)',
                    borderRadius: 'var(--radius-md, 8px)',
                    fontSize: '0.8125rem',
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <span style={{ fontWeight: '500', color: 'var(--color-text-primary, #111113)' }}>
                      {student.name}
                    </span>
                  </div>
                  <span style={{
                    fontWeight: '600',
                    color: ratePercent < 50 ? 'var(--color-danger, #9A3D4A)' : 'var(--color-warning, #7A631C)',
                    flexShrink: 0,
                  }}>
                    {ratePercent}%
                  </span>
                  <span style={{
                    color: 'var(--color-text-muted, #696A6C)',
                    fontSize: '0.75rem',
                    flexShrink: 0,
                  }}>
                    {absences}/{student.totalSessions} absences
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {!loading && !error && data && hasDoneSessions && atRiskStudents.length === 0 && (
        <div style={{
          marginTop: 'var(--space-4, 16px)',
          padding: 'var(--space-3, 12px) var(--space-4, 16px)',
          background: 'color-mix(in srgb, var(--color-success, #257348) 8%, transparent)',
          borderRadius: 'var(--radius-md, 8px)',
          color: 'var(--color-success, #257348)',
          fontSize: '0.8125rem',
          fontWeight: '500',
        }}>
          No students at risk — all attendance rates are above threshold.
        </div>
      )}

      {!loading && !error && data && !hasDoneSessions && (
        <div style={{
          marginTop: 'var(--space-4, 16px)',
          padding: 'var(--space-3, 12px) var(--space-4, 16px)',
          background: 'var(--color-bg-subtle, #F5F5F5)',
          borderRadius: 'var(--radius-md, 8px)',
          color: 'var(--color-text-muted, #696A6C)',
          fontSize: '0.8125rem',
        }}>
          No completed sessions
        </div>
      )}

      {loading && (
        <div style={{
          marginTop: 'var(--space-4, 16px)',
          fontSize: '0.8125rem',
          color: 'var(--color-text-muted, #696A6C)',
        }}>
          Loading attendance data...
        </div>
      )}

      {error && (
        <div style={{
          marginTop: 'var(--space-4, 16px)',
          fontSize: '0.8125rem',
          color: 'var(--color-text-muted, #696A6C)',
        }}>
          Attendance data unavailable
        </div>
      )}

      {!loading && !error && data && hasDoneSessions && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(!expanded);
          }}
          style={{
            marginTop: 'var(--space-4, 16px)',
            padding: 'var(--space-2, 8px) var(--space-3, 12px)',
            background: 'transparent',
            border: '1px solid var(--color-border, #DCDBDD)',
            borderRadius: 'var(--radius-md, 8px)',
            cursor: 'pointer',
            fontSize: '0.8125rem',
            fontWeight: '500',
            color: 'var(--color-text-secondary, #4F5056)',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2, 8px)',
            transition: 'border-color 0.2s',
          }}
          onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--color-border-strong, #CFCFD9)'; }}
          onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--color-border, #DCDBDD)'; }}
        >
          {expanded ? 'Collapse' : 'Expand'}
          <span style={{ fontSize: '0.75rem' }}>{expanded ? '▲' : '▼'}</span>
        </button>
      )}

      {expanded && data && hasDoneSessions && (
        <div style={{ marginTop: 'var(--space-6, 24px)' }} onClick={(e) => e.stopPropagation()}>
          <AttendanceTable report={data} />
        </div>
      )}
    </div>
  );
}
