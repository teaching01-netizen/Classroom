import React from 'react';

export const StatsBar = ({ stats }) => {
  return (
    <div style={{
      display: 'flex',
      gap: 'var(--space-6, 24px)',
      marginBottom: 'var(--space-6, 24px)',
      flexWrap: 'wrap',
    }}>
      {stats.map((stat, index) => (
        <div key={index} style={{
          background: 'var(--color-bg, #FFFFFF)',
          border: '1px solid var(--color-border, #DCDBDD)',
          borderRadius: 'var(--radius-xl, 12px)',
          padding: 'var(--space-4, 16px) var(--space-6, 24px)',
          minWidth: '160px',
          height: '80px',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
        }}>
          <div style={{ fontSize: '24px', fontWeight: 'bold', color: 'var(--color-text-primary, #111113)' }}>
            {stat.value}
          </div>
          <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #4F5056)', marginTop: 'var(--space-1, 4px)' }}>
            {stat.label}
          </div>
        </div>
      ))}
    </div>
  );
};
