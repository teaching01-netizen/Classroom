import React from 'react';

export function AtRiskCallout({ students }) {
  if (!students || students.length === 0) {
    return (
      <div style={{
        padding: 'var(--space-4, 16px)',
        background: 'color-mix(in srgb, var(--color-success, #257348) 8%, transparent)',
        borderRadius: 'var(--radius-md, 8px)',
        marginBottom: 'var(--space-6, 24px)',
        color: 'var(--color-success, #257348)',
        fontSize: '0.875rem',
        fontWeight: '500',
      }}>
        No students at risk — all attendance rates are above threshold.
      </div>
    );
  }

  return (
    <div style={{
      marginBottom: 'var(--space-6, 24px)',
      border: '1px solid var(--color-border, #DCDBDD)',
      borderRadius: 'var(--radius-lg, 12px)',
      overflow: 'hidden',
    }}>
      <div style={{
        padding: 'var(--space-3, 12px) var(--space-4, 16px)',
        background: 'color-mix(in srgb, var(--color-warning, #7A631C) 8%, transparent)',
        borderBottom: '1px solid var(--color-border, #DCDBDD)',
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
          {students.length} student{students.length !== 1 ? 's' : ''} at risk
        </span>
      </div>

      <div style={{ background: 'var(--color-bg, #FFFFFF)' }}>
        {students.map((student, idx) => (
          <div
            key={student.studentId}
            style={{
              padding: 'var(--space-3, 12px) var(--space-4, 16px)',
              borderBottom: idx < students.length - 1 ? '1px solid var(--color-border-subtle, #EEEFF1)' : 'none',
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--space-3, 12px)',
            }}
          >
            <div style={{
              width: '32px',
              height: '32px',
              borderRadius: '50%',
              background: 'var(--color-bg-hover, #F1F2F4)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: '0.75rem',
              fontWeight: '600',
              color: 'var(--color-text-secondary, #4F5056)',
              flexShrink: 0,
            }}>
              {(student.name || '?')[0].toUpperCase()}
            </div>

            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{
                fontSize: '0.875rem',
                fontWeight: '500',
                color: 'var(--color-text-primary, #111113)',
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}>
                {student.name}
                {student.nickname && student.nickname !== student.name && (
                  <span style={{ fontWeight: '400', color: 'var(--color-text-muted, #696A6C)', marginLeft: '4px' }}>
                    ({student.nickname})
                  </span>
                )}
              </div>
              {student.courseName && (
                <div style={{
                  fontSize: '0.75rem',
                  color: 'var(--color-text-muted, #696A6C)',
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}>
                  {student.courseName}
                </div>
              )}
            </div>

            <div style={{
              textAlign: 'right',
              flexShrink: 0,
            }}>
              <div style={{
                fontSize: '0.875rem',
                fontWeight: '600',
                color: 'var(--color-danger, #9A3D4A)',
              }}>
                {Math.round(student.attendanceRate * 100)}%
              </div>
              <div style={{
                fontSize: '0.6875rem',
                color: 'var(--color-text-muted, #696A6C)',
              }}>
                {student.absences}/{student.totalSessions} absences
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
