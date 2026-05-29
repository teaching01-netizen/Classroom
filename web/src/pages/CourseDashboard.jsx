import React, { useState, useMemo } from 'react';
import { useCourses } from '../hooks/useCourses';
import { StatsBar } from '../components/StatsBar';
import { CourseCard } from '../components/CourseCard';

export function CourseDashboard() {
  const { courses, isLoading, isRefreshing, error } = useCourses();
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');

  const filteredCourses = useMemo(() => {
    return courses.filter(course => {
      const matchesSearch = course.name.toLowerCase().includes(searchQuery.toLowerCase());
      const matchesStatus = statusFilter === 'all' || course.status === statusFilter;
      return matchesSearch && matchesStatus;
    });
  }, [courses, searchQuery, statusFilter]);

  const stats = useMemo(() => {
    const activeCourses = courses.filter(c => c.status === 'active').length;
    const totalSessions = courses.reduce((sum, c) => sum + c.total_sessions, 0);
    const totalStudents = courses.reduce((sum, c) => sum + c.enrolled_count, 0);
    const avgAttendance = courses.length > 0
      ? Math.round(courses.reduce((sum, c) => sum + c.avg_attendance_rate, 0) / courses.length * 100)
      : 0;

    return [
      { value: activeCourses, label: 'Active Courses' },
      { value: totalSessions, label: 'Total Sessions' },
      { value: totalStudents, label: 'Students' },
      { value: `${avgAttendance}%`, label: 'Avg Attendance' },
    ];
  }, [courses]);

  if (isLoading) {
    return (
      <div style={{ padding: 'var(--space-xl, 32px)', color: 'var(--text-secondary, #94a3b8)' }}>
        Loading courses...
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
      <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--text-primary, #fff)', marginBottom: 'var(--space-lg, 24px)' }}>
        My Courses
      </h2>

      <StatsBar stats={stats} />

      <div style={{
        display: 'flex',
        gap: 'var(--space-md, 16px)',
        marginBottom: 'var(--space-lg, 24px)',
        flexWrap: 'wrap',
      }}>
        <input
          type="text"
          placeholder="Search courses..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          style={{
            padding: '10px var(--space-md, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'var(--bg-input, #1a1a2e)',
            color: 'var(--text-primary, #eee)',
            fontSize: '14px',
            minWidth: '250px',
          }}
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          style={{
            padding: '10px var(--space-md, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'var(--bg-input, #1a1a2e)',
            color: 'var(--text-primary, #eee)',
            fontSize: '14px',
          }}
        >
          <option value="all">All Status</option>
          <option value="active">Active</option>
          <option value="upcoming">Upcoming</option>
          <option value="finished">Finished</option>
        </select>
      </div>

      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(340px, 1fr))',
        gap: '20px',
      }}>
        {filteredCourses.map((course) => (
          <CourseCard key={course.course_id} course={course} />
        ))}
      </div>

      {filteredCourses.length === 0 && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--text-secondary, #94a3b8)' }}>
          <p style={{ fontSize: '1.25rem' }}>
            {courses.length === 0
              ? 'No courses assigned to you yet'
              : 'No courses match your search'
            }
          </p>
        </div>
      )}
    </div>
  );
}
