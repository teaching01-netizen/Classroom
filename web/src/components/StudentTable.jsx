import React from 'react';
import { StudentRow } from './StudentRow';

export const StudentTable = ({ students, onToggleCheckin }) => {
  return (
    <table
      style={{
        width: '100%',
        borderCollapse: 'collapse',
        background: 'var(--color-bg, #FFFFFF)',
        borderRadius: 'var(--radius-xl, 12px)',
        overflow: 'hidden',
        border: '1px solid var(--color-border, #DCDBDD)',
      }}
    >
      <thead>
        <tr
          style={{
            background: 'var(--color-bg-hover, #F1F2F4)',
            borderBottom: '1px solid var(--color-border, #DCDBDD)',
          }}
        >
          <th
            style={{
              padding: '12px var(--space-4, 16px)',
              textAlign: 'left',
              color: 'var(--color-text-secondary, #4F5056)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          ></th>
          <th
            style={{
              padding: '12px var(--space-4, 16px)',
              textAlign: 'left',
              color: 'var(--color-text-secondary, #4F5056)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            Name
          </th>
          <th
            style={{
              padding: '12px var(--space-4, 16px)',
              textAlign: 'left',
              color: 'var(--color-text-secondary, #4F5056)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            School
          </th>
          <th
            style={{
              padding: '12px var(--space-4, 16px)',
              textAlign: 'left',
              color: 'var(--color-text-secondary, #4F5056)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            Status
          </th>
          <th
            style={{
              padding: '12px var(--space-4, 16px)',
              textAlign: 'center',
              color: 'var(--color-text-secondary, #4F5056)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            Points
          </th>
        </tr>
      </thead>
      <tbody>
        {students.map((student, index) => (
          <StudentRow
            key={student.student_id}
            student={student}
            onToggleCheckin={onToggleCheckin}
            index={index}
          />
        ))}
      </tbody>
    </table>
  );
};
