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
          background: 'var(--color-bg, #FFFFFF)',
          borderRadius: '16px',
          padding: 'var(--space-8, 32px)',
          textAlign: 'center',
          animation: 'scaleIn 0.2s ease',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <img
          src={qrUrl}
          alt="QR Code"
          style={{
            width: 'min(75vw, 420px)',
            height: 'min(75vw, 420px)',
            maxWidth: '100%',
            aspectRatio: '1',
            borderRadius: 'var(--radius-xl, 12px)',
          }}
        />

        {timeLeft !== null && (
          <p
            style={{
              marginTop: 'var(--space-4, 16px)',
              fontSize: '14px',
              color: timeLeft <= 10 ? 'var(--color-danger, #9A3D4A)' : 'var(--color-text-secondary, #4F5056)',
            }}
          >
            Expires in: {timeLeft}s
          </p>
        )}

        <button
          onClick={onClose}
          style={{
            marginTop: 'var(--space-4, 16px)',
            padding: '10px var(--space-6, 24px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: 'none',
            background: 'var(--color-primary-600, #276BF0)',
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
