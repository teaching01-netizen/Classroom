import React from 'react';
import { useCountdown } from '../hooks/useCountdown';

const getStatusColor = (status) => {
  switch (status) {
    case 'Running':
      return 'var(--color-success, #4ade80)';
    case 'Fetching':
      return 'var(--color-warning, #fbbf24)';
    case 'Warning':
      return 'var(--color-warning, #f97316)';
    case 'AuthExpired':
    case 'Stopped':
      return 'var(--color-danger, #ef4444)';
    default:
      return 'var(--text-secondary, #94a3b8)';
  }
};

export const RoomCard = ({ room }) => {
  const timeLeft = useCountdown(room.expires_at);

  const handleStart = async () => {
    try {
      const response = await fetch(`/api/rooms/${room.room_id}/start`, {
        method: 'POST',
      });
      const result = await response.json();
      if (!result.success) {
        alert(`Failed to start room: ${result.error}`);
      }
    } catch (error) {
      console.error('Failed to start room:', error);
      alert('Failed to start room. Check console for details.');
    }
  };

  const handleStop = async () => {
    try {
      const response = await fetch(`/api/rooms/${room.room_id}/stop`, {
        method: 'POST',
      });
      const result = await response.json();
      if (!result.success) {
        alert(`Failed to stop room: ${result.error}`);
      }
    } catch (error) {
      console.error('Failed to stop room:', error);
      alert('Failed to stop room. Check console for details.');
    }
  };

  const handleDelete = async () => {
    if (!confirm('Are you sure you want to delete this room?')) return;
    try {
      const response = await fetch(`/api/rooms/${room.room_id}`, {
        method: 'DELETE',
      });
      const result = await response.json();
      if (!result.success) {
        alert(`Failed to delete room: ${result.error}`);
      }
    } catch (error) {
      console.error('Failed to delete room:', error);
      alert('Failed to delete room. Check console for details.');
    }
  };

  return (
    <div style={{
      background: 'var(--bg-card, #16213e)',
      borderRadius: 'var(--radius-lg, 12px)',
      padding: 'var(--space-lg, 24px)',
      border: '1px solid var(--border-default, #2d3a5a)',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--space-md, 16px)' }}>
        <div>
          <h3 style={{ fontSize: '1.25rem', fontWeight: '600', marginBottom: 'var(--space-xs, 4px)' }}>
            {room.name || room.class_id}
          </h3>
          <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary, #94a3b8)' }}>
            Class ID: {room.class_id}
          </p>
          <a href="/courses" onClick={(e) => e.stopPropagation()} style={{
            fontSize: '0.875rem',
            color: 'var(--color-accent, #6366f1)',
            textDecoration: 'none',
          }}>
            View Sessions →
          </a>
        </div>
        <div style={{
          padding: 'var(--space-xs, 4px) 12px',
          borderRadius: 'var(--radius-full, 9999px)',
          fontSize: '0.75rem',
          fontWeight: '500',
          background: `color-mix(in srgb, ${getStatusColor(room.status)} 12%, transparent)`,
          color: getStatusColor(room.status),
        }}>
          {room.status}
        </div>
      </div>

      {room.qr_url && (
        <div style={{ textAlign: 'center', marginBottom: 'var(--space-md, 16px)' }}>
          <img src={room.qr_url} alt={`QR code for ${room.name || room.class_id}`} style={{ width: '200px', height: '200px', borderRadius: 'var(--radius-md, 8px)' }} />
          {timeLeft !== null && (
            <p style={{ marginTop: 'var(--space-sm, 8px)', fontSize: '0.875rem', color: timeLeft <= 10 ? 'var(--color-danger, #ef4444)' : 'var(--text-secondary, #94a3b8)' }}>
              Expires in: {timeLeft}s
            </p>
          )}
        </div>
      )}

      {room.warning_message && (
        <div style={{
          background: 'color-mix(in srgb, var(--color-warning, #f97316) 12%, transparent)',
          color: 'var(--color-warning, #f97316)',
          padding: '12px',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-md, 16px)',
          fontSize: '0.875rem',
        }}>
          ⚠️ {room.warning_message}
        </div>
      )}

      {room.error_message && (
        <div style={{
          background: 'color-mix(in srgb, var(--color-danger, #ef4444) 12%, transparent)',
          color: 'var(--color-danger, #ef4444)',
          padding: '12px',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-md, 16px)',
          fontSize: '0.875rem',
        }}>
          ❌ {room.error_message}
        </div>
      )}

      <div style={{ display: 'flex', gap: 'var(--space-sm, 8px)' }}>
        {room.status !== 'Running' && room.status !== 'Fetching' && (
          <button
            onClick={handleStart}
            style={{
              flex: 1,
              padding: '10px var(--space-md, 16px)',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              background: 'var(--color-success, #4ade80)',
              color: '#000',
              fontWeight: '500',
              cursor: 'pointer',
              transition: 'background 0.2s',
            }}
          >
            Start
          </button>
        )}

        {(room.status === 'Running' || room.status === 'Fetching') && (
          <button
            onClick={handleStop}
            style={{
              flex: 1,
              padding: '10px var(--space-md, 16px)',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              background: 'var(--color-danger, #ef4444)',
              color: '#fff',
              fontWeight: '500',
              cursor: 'pointer',
              transition: 'background 0.2s',
            }}
          >
            Stop
          </button>
        )}

        <button
          onClick={handleDelete}
          style={{
            padding: '10px var(--space-md, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-danger, #ef4444)',
            background: 'transparent',
            color: 'var(--color-danger, #ef4444)',
            fontWeight: '500',
            cursor: 'pointer',
            transition: 'background 0.2s',
          }}
        >
          Delete
        </button>
      </div>
    </div>
  );
};
