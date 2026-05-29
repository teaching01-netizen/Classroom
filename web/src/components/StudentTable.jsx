import React from 'react';
import { StudentRow } from './StudentRow';

export const StudentTable = ({ students, onToggleCheckin }) => {
  return (
    <table
      style={{
        width: '100%',
        borderCollapse: 'collapse',
        background: 'var(--bg-card, #16213e)',
        borderRadius: 'var(--radius-lg, 12px)',
        overflow: 'hidden',
        border: '1px solid var(--border-default, #2d3a5a)',
      }}
    >
      <thead>
        <tr
          style={{
            background: 'var(--bg-input, #1a1a2e)',
            borderBottom: '1px solid var(--border-default, #2d3a5a)',
          }}
        >
          <th
            style={{
              padding: '12px var(--space-md, 16px)',
              textAlign: 'left',
              color: 'var(--text-secondary, #94a3b8)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          ></th>
          <th
            style={{
              padding: '12px var(--space-md, 16px)',
              textAlign: 'left',
              color: 'var(--text-secondary, #94a3b8)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            Name
          </th>
          <th
            style={{
              padding: '12px var(--space-md, 16px)',
              textAlign: 'left',
              color: 'var(--text-secondary, #94a3b8)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            School
          </th>
          <th
            style={{
              padding: '12px var(--space-md, 16px)',
              textAlign: 'left',
              color: 'var(--text-secondary, #94a3b8)',
              fontWeight: '500',
              fontSize: '12px',
            }}
          >
            Status
          </th>
          <th
            style={{
              padding: '12px var(--space-md, 16px)',
              textAlign: 'center',
              color: 'var(--text-secondary, #94a3b8)',
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
