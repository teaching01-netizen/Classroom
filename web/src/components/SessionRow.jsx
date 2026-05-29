import React from 'react';
import { useNavigate } from 'react-router-dom';
import { StatusBadge } from './StatusBadge';

const getBarColor = (status) => {
  switch (status) {
    case 'active': return 'var(--color-success, #257348)';
    case 'done': return 'var(--color-primary-600, #276BF0)';
    case 'not_started': return 'var(--color-border, #DCDBDD)';
    case 'auth_error': return 'var(--color-warning, #7A631C)';
    default: return 'var(--color-border, #DCDBDD)';
  }
};

export const SessionRow = ({ session, courseId }) => {
  const navigate = useNavigate();

  const handleClick = () => {
    navigate(`/courses/${courseId}/sessions/${session.session_id}`);
  };

  return (
    <tr
      onClick={handleClick}
      style={{
        height: '56px',
        cursor: 'pointer',
        transition: 'background 0.15s ease',
      }}
      onMouseEnter={(e) => e.currentTarget.style.background = 'var(--color-bg-hover, #F1F2F4)'}
      onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}
    >
      <td style={{ position: 'relative', paddingLeft: 'var(--space-4, 16px)' }}>
        <div style={{
          position: 'absolute',
          left: 0,
          top: 0,
          bottom: 0,
          width: '4px',
          background: getBarColor(session.status),
        }} />
        {session.session_number}
      </td>
      <td style={{ color: 'var(--color-text-primary, #111113)', fontWeight: '500' }}>{session.name}</td>
      <td style={{ color: 'var(--color-text-secondary, #4F5056)' }}>{session.date}</td>
      <td><StatusBadge status={session.status} /></td>
      <td style={{
        fontFamily: 'monospace',
        color: 'var(--color-text-primary, #111113)',
        textAlign: 'center',
      }}>
        {session.total_students > 0
          ? `${session.checked_in_count}/${session.total_students}`
          : '—'
        }
      </td>
      <td style={{ color: 'var(--color-text-secondary, #4F5056)', textAlign: 'right', paddingRight: 'var(--space-4, 16px)' }}>→</td>
    </tr>
  );
};
