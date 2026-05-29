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
        background: 'var(--bg-input, #1a1a2e)',
        color: 'var(--text-secondary, #94a3b8)',
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
      background: 'var(--bg-input, #1a1a2e)',
      padding: 'var(--space-lg, 24px)',
    }}>
      <h1 style={{ fontSize: '2.5rem', fontWeight: '700', marginBottom: 'var(--space-xl, 32px)', color: 'var(--text-primary, #eee)' }}>
        {room.name || room.class_id}
      </h1>
      <img src={room.qr_url} alt="QR Code" style={{ width: '400px', height: '400px', borderRadius: '16px', marginBottom: 'var(--space-lg, 24px)' }} />
      {timeLeft !== null && (
        <div style={{ fontSize: '3rem', fontWeight: '700', color: timeLeft <= 10 ? 'var(--color-danger, #ef4444)' : 'var(--color-success, #4ade80)' }}>
          {timeLeft}s
        </div>
      )}
    </div>
  );
};
