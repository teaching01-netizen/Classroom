import React from 'react';
import { Link } from 'react-router-dom';

export const BackBreadcrumb = ({ to, label }) => {
  return (
    <Link
      to={to}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--space-2, 8px)',
        color: 'var(--color-text-secondary, #4F5056)',
        textDecoration: 'none',
        fontSize: '14px',
        marginBottom: 'var(--space-4, 16px)',
        transition: 'color 0.2s',
      }}
      onMouseEnter={(e) => e.currentTarget.style.color = 'var(--color-text-primary, #111113)'}
      onMouseLeave={(e) => e.currentTarget.style.color = 'var(--color-text-secondary, #4F5056)'}
    >
      ← {label}
    </Link>
  );
};
