import React from 'react';

const getStatusIcon = (checkedIn) => {
  return checkedIn
    ? { icon: '✅', color: 'var(--color-success, #4ade80)', label: 'In' }
    : { icon: '⏳', color: 'var(--text-secondary, #94a3b8)', label: 'Not' };
};

export const StudentRow = ({ student, onToggleCheckin, index }) => {
  const status = getStatusIcon(student.checked_in);
  const isEven = index % 2 === 0;

  return (
    <tr
      style={{
        height: '52px',
        background: isEven ? 'transparent' : '#ffffff05',
        cursor: 'pointer',
        transition: 'background 0.15s ease',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--bg-card-hover, #1a1a2e)')}
      onMouseLeave={(e) =>
        (e.currentTarget.style.background = isEven ? 'transparent' : '#ffffff05')
      }
      onClick={() => onToggleCheckin(student.student_id, !student.checked_in)}
    >
      <td style={{ padding: '0 var(--space-md, 16px)' }}>
        <img
          src={
            student.avatar_url ||
            `https://ui-avatars.com/api/?name=${encodeURIComponent(student.name)}&background=6366f1&color=fff`
          }
          alt={student.name}
          style={{
            width: '36px',
            height: '36px',
            borderRadius: '50%',
            objectFit: 'cover',
          }}
        />
      </td>
      <td style={{ color: 'var(--text-primary, #fff)', fontWeight: '500' }}>{student.name}</td>
      <td style={{ color: 'var(--text-secondary, #94a3b8)' }}>{student.school}</td>
      <td>
        <span style={{ color: status.color }}>
          {status.icon} {status.label}
        </span>
      </td>
      <td style={{ textAlign: 'center', color: 'var(--text-primary, #fff)' }}>{student.participation_points}</td>
    </tr>
  );
};
