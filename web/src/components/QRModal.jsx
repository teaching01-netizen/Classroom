import React, { useEffect } from 'react';
import { useCountdown } from '../hooks/useCountdown';

export const QRModal = ({ qrUrl, expiresIn, onClose }) => {
  const timeLeft = useCountdown(expiresIn);

  useEffect(() => {
    const handleEscape = (e) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [onClose]);

  if (!qrUrl) return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        background: 'rgba(0, 0, 0, 0.8)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
        animation: 'fadeIn 0.2s ease',
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'var(--bg-card, #16213e)',
          borderRadius: '16px',
          padding: 'var(--space-xl, 32px)',
          textAlign: 'center',
          animation: 'scaleIn 0.2s ease',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <img
          src={qrUrl}
          alt="QR Code"
          style={{
            width: '300px',
            height: '300px',
            borderRadius: 'var(--radius-lg, 12px)',
          }}
        />

        {timeLeft !== null && (
          <p
            style={{
              marginTop: 'var(--space-md, 16px)',
              fontSize: '14px',
              color: timeLeft <= 10 ? 'var(--color-danger, #ef4444)' : 'var(--text-secondary, #94a3b8)',
            }}
          >
            Expires in: {timeLeft}s
          </p>
        )}

        <button
          onClick={onClose}
          style={{
            marginTop: 'var(--space-md, 16px)',
            padding: '10px var(--space-lg, 24px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: 'none',
            background: 'var(--color-accent, #6366f1)',
            color: '#fff',
            fontWeight: '500',
            cursor: 'pointer',
          }}
        >
          Close
        </button>
      </div>

      <style>{`
        @keyframes fadeIn {
          from { opacity: 0; }
          to { opacity: 1; }
        }
        @keyframes scaleIn {
          from { transform: scale(0.95); opacity: 0; }
          to { transform: scale(1); opacity: 1; }
        }
      `}</style>
    </div>
  );
};
