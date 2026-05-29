import React, { useEffect } from 'react';
import { useCountdown } from '../hooks/useCountdown';

export const QRModal = ({ qrUrl, expiresIn, onClose, courseId, roomName, className, checkedCount, totalCount }) => {
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
        {/* Info header — course/session context */}
        <div style={{
          marginBottom: 'var(--space-4, 16px)',
          paddingBottom: 'var(--space-3, 12px)',
          borderBottom: '1px solid var(--color-border, #DCDBDD)',
        }}>
          {className && (
            <div style={{ fontSize: '16px', fontWeight: '600', color: 'var(--color-text-primary, #111113)', marginBottom: 'var(--space-1, 4px)' }}>
              {className}
            </div>
          )}
          <div style={{ fontSize: '13px', color: 'var(--color-text-secondary, #4F5056)' }}>
            {[courseId, roomName].filter(Boolean).join(' · ')}
          </div>
        </div>

        {/* Live stats — checked-in counter */}
        {(checkedCount !== undefined && totalCount !== undefined) && (
          <div style={{
            marginBottom: 'var(--space-4, 16px)',
            paddingBottom: 'var(--space-3, 12px)',
            borderBottom: '1px solid var(--color-border, #DCDBDD)',
          }}>
            <div style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginBottom: 'var(--space-2, 8px)',
            }}>
              <span style={{ fontSize: '14px', fontWeight: '500', color: 'var(--color-text-primary, #111113)' }}>
                Checked in
              </span>
              <span style={{ fontSize: '14px', fontWeight: '600', color: 'var(--color-text-primary, #111113)' }}>
                {checkedCount}/{totalCount}
              </span>
            </div>
            <div style={{
              width: '100%',
              height: '6px',
              background: 'var(--color-bg-subtle, #F5F5F5)',
              borderRadius: 'var(--radius-full, 9999px)',
              overflow: 'hidden',
            }}>
              <div style={{
                width: `${totalCount > 0 ? (checkedCount / totalCount) * 100 : 0}%`,
                height: '100%',
                background: checkedCount === totalCount ? 'var(--color-success, #257348)' : 'var(--color-primary-600, #276BF0)',
                borderRadius: 'var(--radius-full, 9999px)',
                transition: 'width 0.3s ease, background 0.3s ease',
              }} />
            </div>
          </div>
        )}

        <img
          src={qrUrl}
          alt="QR Code"
          style={{
            width: 'min(75vw, 420px)',
            height: 'auto',
            maxWidth: '100%',
            aspectRatio: '1',
            borderRadius: 'var(--radius-xl, 12px)',
          }}
        />

        {/* Instructional CTA */}
        <p style={{
          marginTop: 'var(--space-3, 12px)',
          marginBottom: 'var(--space-1, 4px)',
          fontSize: '14px',
          color: 'var(--color-text-secondary, #4F5056)',
        }}>
          Point your camera at the QR code to check in
        </p>

        {timeLeft !== null && (
          <div style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: '6px',
            marginTop: 'var(--space-2, 8px)',
            padding: '4px 12px',
            borderRadius: 'var(--radius-full, 9999px)',
            fontSize: '13px',
            fontWeight: '500',
            background: timeLeft <= 10 ? 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)' : 'var(--color-bg-subtle, #F5F5F5)',
            color: timeLeft <= 10 ? 'var(--color-danger, #9A3D4A)' : 'var(--color-text-secondary, #4F5056)',
          }}>
            {timeLeft <= 10 && <span>⚠️</span>}
            {timeLeft <= 0 ? 'Expired' : `Expires in ${timeLeft}s`}
          </div>
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
