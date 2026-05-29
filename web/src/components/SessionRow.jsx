import React from 'react';
import { useNavigate } from 'react-router-dom';
import { StatusBadge } from './StatusBadge';

const getBarColor = (status) => {
  switch (status) {
    case 'active': return 'var(--color-success, #4ade80)';
    case 'done': return 'var(--color-accent, #6366f1)';
    case 'not_started': return 'var(--border-default, #2d3a5a)';
    case 'auth_error': return 'var(--color-warning, #f97316)';
    default: return 'var(--border-default, #2d3a5a)';
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
      onMouseEnter={(e) => e.currentTarget.style.background = 'var(--bg-card-hover, #1a1a2e)'}
      onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}
    >
      <td style={{ position: 'relative', paddingLeft: 'var(--space-md, 16px)' }}>
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
      <td style={{ color: 'var(--text-primary, #fff)', fontWeight: '500' }}>{session.name}</td>
      <td style={{ color: 'var(--text-secondary, #94a3b8)' }}>{session.date}</td>
      <td><StatusBadge status={session.status} /></td>
      <td style={{
        fontFamily: 'monospace',
        color: 'var(--text-primary, #fff)',
        textAlign: 'center',
      }}>
        {session.total_students > 0
          ? `${session.checked_in_count}/${session.total_students}`
          : '—'
        }
      </td>
      <td style={{ color: 'var(--text-secondary, #94a3b8)', textAlign: 'right', paddingRight: 'var(--space-md, 16px)' }}>→</td>
    </tr>
  );
};
