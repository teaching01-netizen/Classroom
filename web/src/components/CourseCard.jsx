import React from 'react';
import { useNavigate } from 'react-router-dom';
import { usePinnedCoursesStore, selectIsPinned } from '../store/usePinnedCoursesStore';

const getStatusColor = (status) => {
  switch (status) {
    case 'active': return 'var(--color-success, #257348)';
    case 'upcoming': return 'var(--color-info, #315EBA)';
    case 'finished': return 'var(--color-text-secondary, #4F5056)';
    default: return 'var(--color-text-secondary, #4F5056)';
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
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          handleClick();
        }
      }}
      role="button"
      tabIndex={0}
      style={{
        background: 'var(--color-bg, #FFFFFF)',
        border: '1px solid var(--color-border, #DCDBDD)',
        borderRadius: 'var(--radius-xl, 12px)',
        padding: '20px',
        cursor: 'pointer',
        transition: 'transform 0.2s ease, box-shadow 0.2s ease',
        width: '100%',
        maxWidth: '340px',
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.transform = 'translateY(-2px)';
        e.currentTarget.style.boxShadow = 'var(--shadow-md, 0 8px 24px rgba(16, 24, 40, 0.10))';
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.transform = 'translateY(0)';
        e.currentTarget.style.boxShadow = 'none';
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2, 8px)', marginBottom: '12px' }}>
        <div style={{
          width: '8px',
          height: '8px',
          borderRadius: '50%',
          background: getStatusColor(course.status),
        }} />
        <div style={{ fontSize: '16px', fontWeight: '600', color: 'var(--color-text-primary, #111113)', flex: 1 }}>
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
            color: isPinned ? 'var(--color-warning, #7A631C)' : 'var(--color-text-secondary, #4F5056)',
            transition: 'color 0.2s',
          }}
          title={isPinned ? 'Unpin course' : 'Pin course'}
        >
          {isPinned ? '★' : '☆'}
        </button>
      </div>

      {attendancePercent > 0 && (
        <div style={{
          height: '6px',
          background: 'var(--color-border, #DCDBDD)',
          borderRadius: '3px',
          marginBottom: '12px',
          overflow: 'hidden',
        }}>
          <div style={{
            height: '100%',
            width: `${attendancePercent}%`,
            background: 'var(--color-success, #257348)',
            borderRadius: '3px',
            transition: 'width 0.6s ease-out',
          }} />
        </div>
      )}

      <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #4F5056)', marginBottom: '4px' }}>
        📅 {course.start_date} - {course.end_date}
      </div>
      <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #4F5056)', marginBottom: '4px' }}>
        👥 {course.enrolled_count} students
      </div>
      <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #4F5056)', marginBottom: '8px' }}>
        📋 {course.completed_sessions}/{course.total_sessions} sessions
      </div>

      <div style={{
        fontSize: '14px',
        fontWeight: '600',
        color: attendancePercent === 0 ? 'var(--color-text-secondary, #4F5056)' : attendancePercent >= 80 ? 'var(--color-success, #257348)' : attendancePercent >= 50 ? 'var(--color-warning, #7A631C)' : 'var(--color-danger, #9A3D4A)',
      }}>
        {attendancePercent > 0 ? `${attendancePercent}% attendance` : '— attendance'}
      </div>
    </div>
  );
};
