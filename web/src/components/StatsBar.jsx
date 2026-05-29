import React from 'react';

export const StatsBar = ({ stats }) => {
  return (
    <div style={{
      display: 'flex',
      gap: 'var(--space-lg, 24px)',
      marginBottom: 'var(--space-lg, 24px)',
      flexWrap: 'wrap',
    }}>
      {stats.map((stat, index) => (
        <div key={index} style={{
          background: 'var(--bg-card, #16213e)',
          border: '1px solid var(--border-default, #2d3a5a)',
          borderRadius: 'var(--radius-lg, 12px)',
          padding: 'var(--space-md, 16px) var(--space-lg, 24px)',
          minWidth: '160px',
          height: '80px',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
        }}>
          <div style={{ fontSize: '24px', fontWeight: 'bold', color: 'var(--text-primary, #fff)' }}>
            {stat.value}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-secondary, #94a3b8)', marginTop: 'var(--space-xs, 4px)' }}>
            {stat.label}
          </div>
        </div>
      ))}
    </div>
  );
};
