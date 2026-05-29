import React from 'react';

const getStatusIcon = (checkedIn) => {
  return checkedIn
    ? { icon: '✅', color: 'var(--color-success, #257348)', label: 'In' }
    : { icon: '⏳', color: 'var(--color-text-secondary, #4F5056)', label: 'Not' };
};

export const StudentRow = ({ student, onToggleCheckin, index }) => {
  const status = getStatusIcon(student.checked_in);
  const isEven = index % 2 === 0;

  return (
    <tr
      style={{
        height: '52px',
        background: isEven ? 'transparent' : 'rgba(0, 0, 0, 0.02)',
        cursor: 'pointer',
        transition: 'background 0.15s ease',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--color-bg-hover, #F1F2F4)')}
      onMouseLeave={(e) =>
        (e.currentTarget.style.background = isEven ? 'transparent' : 'rgba(0, 0, 0, 0.02)')
      }
      onClick={() => onToggleCheckin(student.student_id, !student.checked_in)}
    >
      <td style={{ padding: '0 var(--space-4, 16px)' }}>
        <img
          src={
            student.avatar_url ||
            `https://ui-avatars.com/api/?name=${encodeURIComponent(student.name)}&background=276BF0&color=fff`
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
      <td style={{ color: 'var(--color-text-primary, #111113)', fontWeight: '500' }}>{student.name}</td>
      <td style={{ color: 'var(--color-text-secondary, #4F5056)' }}>{student.school}</td>
      <td>
        <span style={{ color: status.color }}>
          {status.icon} {status.label}
        </span>
      </td>
      <td style={{ textAlign: 'center', color: 'var(--color-text-primary, #111113)' }}>{student.participation_points}</td>
    </tr>
  );
};
