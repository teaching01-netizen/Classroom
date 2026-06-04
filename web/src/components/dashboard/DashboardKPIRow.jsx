import React from 'react';

export function DashboardKPIRow({ data }) {
  if (!data) return null;

  const kpis = [
    { label: 'At Risk', value: data.atRiskCount, color: data.atRiskCount > 0 ? '#9A3D4A' : '#257348' },
    { label: 'Avg Attendance', value: `${Math.round((data.avgAttendanceRate || 0) * 100)}%`, color: '#111113' },
    { label: 'Total Students', value: data.totalStudents, color: '#111113' },
    { label: 'Courses', value: data.totalCourses, color: '#111113' },
  ];

  return (
    <div style={{
      display: 'flex',
      gap: 'var(--space-4, 16px)',
      marginBottom: 'var(--space-6, 24px)',
      flexWrap: 'wrap',
    }}>
      {kpis.map((kpi) => (
        <div
          key={kpi.label}
          style={{
            flex: '1 1 140px',
            padding: 'var(--space-4, 16px) var(--space-5, 20px)',
            background: 'var(--color-bg, #FFFFFF)',
            borderRadius: 'var(--radius-lg, 12px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            minWidth: '120px',
          }}
        >
          <div style={{
            fontSize: '1.5rem',
            fontWeight: '700',
            color: kpi.color,
            marginBottom: 'var(--space-1, 4px)',
          }}>
            {kpi.value}
          </div>
          <div style={{
            fontSize: '0.8125rem',
            color: 'var(--color-text-muted, #696A6C)',
            fontWeight: '500',
          }}>
            {kpi.label}
          </div>
        </div>
      ))}
    </div>
  );
}
