import React from 'react';
import { useCountdown } from '../hooks/useCountdown';

export const QRDisplay = ({ room }) => {
  const timeLeft = useCountdown(room.expires_at);

  if (!room.qr_url) {
    return (
      <div style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        background: 'var(--color-bg-app, #FBFBFB)',
        color: 'var(--color-text-secondary, #4F5056)',
      }}>
        <p style={{ fontSize: '1.5rem' }}>No QR Code Available</p>
      </div>
    );
  }

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: '100vh',
      background: 'var(--color-bg-app, #FBFBFB)',
      padding: 'var(--space-6, 24px)',
    }}>
      <div style={{ textAlign: 'center', marginBottom: 'var(--space-6, 24px)' }}>
        <h1 style={{ fontSize: '2.5rem', fontWeight: '700', color: 'var(--color-text-primary, #111113)' }}>
          {room.name || room.class_id}
        </h1>
        {room.class_id && (
          <p style={{ fontSize: '1rem', color: 'var(--color-text-secondary, #4F5056)', marginTop: 'var(--space-1, 4px)' }}>
            Class ID: {room.class_id}
          </p>
        )}
      </div>
      <img src={room.qr_url} alt="QR Code" style={{ width: 'min(80vw, 520px)', height: 'auto', maxWidth: '100%', aspectRatio: '1', borderRadius: '16px', marginBottom: 'var(--space-3, 12px)' }} />
      <p style={{
        fontSize: '1.125rem',
        color: 'var(--color-text-secondary, #4F5056)',
        marginBottom: 'var(--space-3, 12px)',
      }}>
        Scan to check in
      </p>
      {timeLeft !== null && (
        <div style={{ fontSize: '3rem', fontWeight: '700', color: timeLeft <= 10 ? 'var(--color-danger, #9A3D4A)' : 'var(--color-success, #257348)' }}>
          {timeLeft}s
        </div>
      )}
    </div>
  );
};
