import React from 'react';
import { SessionRow } from './SessionRow';

export const SessionTable = ({ sessions, courseId }) => {
  return (
    <table style={{
      width: '100%',
      borderCollapse: 'collapse',
      background: 'var(--color-bg, #FFFFFF)',
      borderRadius: 'var(--radius-xl, 12px)',
      overflow: 'hidden',
      border: '1px solid var(--color-border, #DCDBDD)',
    }}>
      <thead>
        <tr style={{
          background: 'var(--color-bg-hover, #F1F2F4)',
          borderBottom: '1px solid var(--color-border, #DCDBDD)',
        }}>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'left', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}>#</th>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'left', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}>Session Name</th>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'left', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}>Date</th>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'left', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}>Status</th>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'center', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}>Attendance</th>
          <th style={{ padding: '12px var(--space-4, 16px)', textAlign: 'right', color: 'var(--color-text-secondary, #4F5056)', fontWeight: '500', fontSize: '12px' }}></th>
        </tr>
      </thead>
      <tbody>
        {sessions.map((session) => (
          <SessionRow
            key={session.session_id}
            session={session}
            courseId={courseId}
          />
        ))}
      </tbody>
    </table>
  );
};
