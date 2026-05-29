import React from 'react';
import { useNavigate } from 'react-router-dom';
import { usePinnedCoursesStore, selectIsPinned } from '../store/usePinnedCoursesStore';

const getStatusColor = (status) => {
  switch (status) {
    case 'active': return 'var(--color-success, #4ade80)';
    case 'upcoming': return 'var(--color-info, #60a5fa)';
    case 'finished': return 'var(--text-secondary, #94a3b8)';
    default: return 'var(--text-secondary, #94a3b8)';
  }
};

export const CourseCard = ({ course }) => {
  const navigate = useNavigate();
  const isPinned = usePinnedCoursesStore(selectIsPinned(course.course_id));
  const toggleCourse = usePinnedCoursesStore((state) => state.toggleCourse);

  const handleClick = () => {
    navigate(`/courses/${course.course_id}/sessions`);
  };

  const attendancePercent = Math.min(Math.max(Math.round(course.avg_attendance_rate * 100), 0), 100);

  return (
    <div
      onClick={handleClick}
      style={{
        background: 'var(--bg-card, #16213e)',
        border: '1px solid var(--border-default, #2d3a5a)',
        borderRadius: 'var(--radius-lg, 12px)',
        padding: '20px',
        cursor: 'pointer',
        transition: 'transform 0.2s ease, box-shadow 0.2s ease',
        width: '100%',
        maxWidth: '340px',
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.transform = 'translateY(-2px)';
        e.currentTarget.style.boxShadow = 'var(--shadow-card-hover, 0 8px 24px rgba(0,0,0,0.4))';
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.transform = 'translateY(0)';
        e.currentTarget.style.boxShadow = 'none';
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-sm, 8px)', marginBottom: '12px' }}>
        <div style={{
          width: '8px',
          height: '8px',
          borderRadius: '50%',
          background: getStatusColor(course.status),
        }} />
        <div style={{ fontSize: '16px', fontWeight: '600', color: 'var(--text-primary, #fff)', flex: 1 }}>
          {course.name}
        </div>
        <button
          onClick={(e) => {
            e.stopPropagation();
            toggleCourse(course.course_id);
          }}
          aria-label={isPinned ? 'Unpin course' : 'Pin course'}
          aria-pressed={isPinned}
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            fontSize: '18px',
            padding: '4px',
            color: isPinned ? 'var(--color-warning, #fbbf24)' : 'var(--text-secondary, #94a3b8)',
            transition: 'color 0.2s',
          }}
          title={isPinned ? 'Unpin course' : 'Pin course'}
        >
          {isPinned ? '★' : '☆'}
        </button>
      </div>

      <div style={{
        height: '6px',
        background: 'var(--border-default, #2d3a5a)',
        borderRadius: '3px',
        marginBottom: '12px',
        overflow: 'hidden',
      }}>
        <div style={{
          height: '100%',
          width: `${attendancePercent}%`,
          background: 'var(--color-success, #4ade80)',
          borderRadius: '3px',
          transition: 'width 0.6s ease-out',
        }} />
      </div>

      <div style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)', marginBottom: '4px' }}>
        📅 {course.start_date} - {course.end_date}
      </div>
      <div style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)', marginBottom: '4px' }}>
        👥 {course.enrolled_count} students
      </div>
      <div style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)', marginBottom: '8px' }}>
        📋 {course.completed_sessions}/{course.total_sessions} sessions
      </div>

      <div style={{
        fontSize: '14px',
        fontWeight: '600',
        color: attendancePercent >= 80 ? 'var(--color-success, #4ade80)' : attendancePercent >= 50 ? 'var(--color-warning, #fbbf24)' : 'var(--color-danger, #ef4444)',
      }}>
        {attendancePercent}% attendance
      </div>
    </div>
  );
};
