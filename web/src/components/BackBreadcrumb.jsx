import React from 'react';
import { Link } from 'react-router-dom';

export const BackBreadcrumb = ({ to, label }) => {
  return (
    <Link
      to={to}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 'var(--space-sm, 8px)',
        color: 'var(--text-secondary, #94a3b8)',
        textDecoration: 'none',
        fontSize: '14px',
        marginBottom: 'var(--space-md, 16px)',
        transition: 'color 0.2s',
      }}
      onMouseEnter={(e) => e.currentTarget.style.color = 'var(--text-primary, #fff)'}
      onMouseLeave={(e) => e.currentTarget.style.color = 'var(--text-secondary, #94a3b8)'}
    >
      ← {label}
    </Link>
  );
};
