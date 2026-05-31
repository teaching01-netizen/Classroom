import React from 'react';

const getPageNumbers = (currentPage, totalPages) => {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, i) => i + 1);
  }

  const pages = [];
  pages.push(1);

  if (currentPage > 3) {
    pages.push('...');
  }

  const start = Math.max(2, currentPage - 1);
  const end = Math.min(totalPages - 1, currentPage + 1);

  for (let i = start; i <= end; i++) {
    pages.push(i);
  }

  if (currentPage < totalPages - 2) {
    pages.push('...');
  }

  pages.push(totalPages);
  return pages;
};

const buttonBase = {
  background: 'var(--color-bg, #FFFFFF)',
  border: '1px solid var(--color-border, #DCDBDD)',
  borderRadius: 'var(--radius-md, 8px)',
  padding: 'var(--space-1, 4px) var(--space-2, 8px)',
  fontSize: '13px',
  cursor: 'pointer',
  color: 'var(--color-text-primary, #111113)',
  fontFamily: 'inherit',
  lineHeight: '1.4',
};

const buttonHover = {
  background: 'var(--color-bg-hover, #F1F2F4)',
};

const buttonActive = {
  background: 'var(--color-primary, #111113)',
  color: '#FFFFFF',
  borderColor: 'var(--color-primary, #111113)',
};

const disabledStyle = {
  opacity: 0.4,
  cursor: 'not-allowed',
};

export const Pagination = ({ currentPage, totalItems, perPage, onPageChange }) => {
  const safePerPage = Math.max(1, perPage || 1);
  const totalPages = Math.max(1, Math.ceil(totalItems / safePerPage));
  const startItem = totalItems === 0 ? 0 : (currentPage - 1) * safePerPage + 1;
  const endItem = Math.min(currentPage * safePerPage, totalItems);
  const pageNumbers = getPageNumbers(currentPage, totalPages);

  const handlePageChange = (page) => {
    if (page >= 1 && page <= totalPages && page !== currentPage) {
      onPageChange(page);
    }
  };

  return (
    <nav aria-label="Pagination" style={{
      display: 'flex',
      justifyContent: 'space-between',
      alignItems: 'center',
      padding: 'var(--space-4, 16px)',
      background: 'var(--color-bg, #FFFFFF)',
      border: '1px solid var(--color-border, #DCDBDD)',
      borderRadius: 'var(--radius-xl, 12px)',
      flexWrap: 'wrap',
      gap: 'var(--space-4, 16px)',
    }}>
      <div style={{
        fontSize: '13px',
        color: 'var(--color-text-secondary, #4F5056)',
      }}>
        Showing {startItem}–{endItem} of {totalItems}
      </div>

      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-1, 4px)',
      }}>
        <button
          onClick={() => handlePageChange(currentPage - 1)}
          disabled={currentPage === 1}
          style={{
            ...buttonBase,
            ...(currentPage === 1 ? disabledStyle : {}),
          }}
          onMouseEnter={(e) => {
            if (currentPage !== 1) e.currentTarget.style.background = buttonHover.background;
          }}
          onMouseLeave={(e) => {
            if (currentPage !== 1) e.currentTarget.style.background = buttonBase.background;
          }}
        >
          Previous
        </button>

        {pageNumbers.map((page, index) =>
          page === '...' ? (
            <span key={`ellipsis-${page}-${index}`} style={{
              padding: 'var(--space-1, 4px) var(--space-2, 8px)',
              fontSize: '13px',
              color: 'var(--color-text-secondary, #4F5056)',
            }}>
              ...
            </span>
          ) : (
            <button
              key={page}
              onClick={() => handlePageChange(page)}
              style={{
                ...buttonBase,
                ...(page === currentPage ? buttonActive : {}),
                minWidth: '32px',
                textAlign: 'center',
              }}
              onMouseEnter={(e) => {
                if (page !== currentPage) e.currentTarget.style.background = buttonHover.background;
              }}
              onMouseLeave={(e) => {
                if (page !== currentPage) e.currentTarget.style.background = buttonBase.background;
              }}
            >
              {page}
            </button>
          )
        )}

        <button
          onClick={() => handlePageChange(currentPage + 1)}
          disabled={currentPage === totalPages}
          style={{
            ...buttonBase,
            ...(currentPage === totalPages ? disabledStyle : {}),
          }}
          onMouseEnter={(e) => {
            if (currentPage !== totalPages) e.currentTarget.style.background = buttonHover.background;
          }}
          onMouseLeave={(e) => {
            if (currentPage !== totalPages) e.currentTarget.style.background = buttonBase.background;
          }}
        >
          Next
        </button>
      </div>
    </nav>
  );
};
