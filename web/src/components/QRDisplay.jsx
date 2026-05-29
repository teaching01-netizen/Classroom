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
        background: '#1a1a2e',
        color: '#94a3b8',
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
      background: '#1a1a2e',
      padding: '24px',
    }}>
      <h1 style={{ fontSize: '2.5rem', fontWeight: '700', marginBottom: '32px', color: '#eee' }}>
        {room.name || room.class_id}
      </h1>
      <img src={room.qr_url} alt="QR Code" style={{ width: '400px', height: '400px', borderRadius: '16px', marginBottom: '24px' }} />
      {timeLeft !== null && (
        <div style={{ fontSize: '3rem', fontWeight: '700', color: timeLeft <= 10 ? '#ef4444' : '#4ade80' }}>
          {timeLeft}s
        </div>
      )}
    </div>
  );
};
