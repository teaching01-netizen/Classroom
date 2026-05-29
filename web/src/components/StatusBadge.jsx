import React from 'react';

const statusConfig = {
  active: { bg: 'rgba(74, 222, 128, 0.12)', color: 'var(--color-success, #4ade80)', icon: '●', label: 'Active' },
  done: { bg: 'rgba(99, 102, 241, 0.12)', color: 'var(--color-accent, #6366f1)', icon: '✓', label: 'Done' },
  not_started: { bg: 'rgba(45, 58, 90, 0.12)', color: 'var(--text-secondary, #94a3b8)', icon: '○', label: 'Pending' },
  auth_error: { bg: 'rgba(249, 115, 22, 0.12)', color: 'var(--color-warning, #f97316)', icon: '⚠', label: 'Error' },
};

export const StatusBadge = ({ status }) => {
  const config = statusConfig[status] || statusConfig.not_started;

  return (
    <span style={{
      display: 'inline-flex',
      alignItems: 'center',
      gap: '6px',
      padding: 'var(--space-xs, 4px) 12px',
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
