import React from 'react';

const statusConfig = {
  active: { bg: 'var(--color-success-bg, #DCF3E5)', color: 'var(--color-success, #257348)', icon: '●', label: 'Active' },
  done: { bg: 'var(--color-primary-soft, #EAF0FE)', color: 'var(--color-primary-600, #276BF0)', icon: '✓', label: 'Done' },
  not_started: { bg: 'var(--color-bg-subtle, #F5F5F5)', color: 'var(--color-text-secondary, #4F5056)', icon: '○', label: 'Pending' },
  auth_error: { bg: 'var(--color-warning-bg, #FAF0C4)', color: 'var(--color-warning, #7A631C)', icon: '⚠', label: 'Error' },
};

export const StatusBadge = ({ status }) => {
  const config = statusConfig[status] || statusConfig.not_started;

  return (
    <span style={{
      display: 'inline-flex',
      alignItems: 'center',
      gap: '6px',
      padding: 'var(--space-1, 4px) 12px',
      borderRadius: 'var(--radius-full, 9999px)',
      fontSize: '12px',
      fontWeight: '500',
      background: config.bg,
      color: config.color,
    }}>
      {config.icon} {config.label}
    </span>
  );
};
