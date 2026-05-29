import React from 'react';
import { useCountdown } from '../hooks/useCountdown';

const getStatusColor = (status) => {
  switch (status) {
    case 'Running':
      return 'var(--color-success, #257348)';
    case 'Fetching':
      return 'var(--color-warning, #7A631C)';
    case 'Warning':
      return 'var(--color-warning, #7A631C)';
    case 'AuthExpired':
    case 'Stopped':
      return 'var(--color-danger, #9A3D4A)';
    default:
      return 'var(--color-text-secondary, #4F5056)';
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
      background: 'var(--color-bg, #FFFFFF)',
      borderRadius: 'var(--radius-xl, 12px)',
      padding: 'var(--space-6, 24px)',
      border: '1px solid var(--color-border, #DCDBDD)',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--space-4, 16px)' }}>
        <div>
          <h3 style={{ fontSize: '1.25rem', fontWeight: '600', marginBottom: 'var(--space-1, 4px)' }}>
            {room.name || room.class_id}
          </h3>
          <p style={{ fontSize: '0.875rem', color: 'var(--color-text-secondary, #4F5056)' }}>
            Class ID: {room.class_id}
          </p>
          <a href="/courses" onClick={(e) => e.stopPropagation()} style={{
            fontSize: '0.875rem',
            color: 'var(--color-primary-600, #276BF0)',
            textDecoration: 'none',
          }}>
            View Sessions →
          </a>
        </div>
        <div style={{
          padding: 'var(--space-1, 4px) 12px',
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
        <div style={{ textAlign: 'center', marginBottom: 'var(--space-4, 16px)' }}>
          <img src={room.qr_url} alt={`QR code for ${room.name || room.class_id}`} style={{ width: '200px', height: '200px', borderRadius: 'var(--radius-md, 8px)' }} />
          {timeLeft !== null && (
            <p style={{ marginTop: 'var(--space-2, 8px)', fontSize: '0.875rem', color: timeLeft <= 10 ? 'var(--color-danger, #9A3D4A)' : 'var(--color-text-secondary, #4F5056)' }}>
              Expires in: {timeLeft}s
            </p>
          )}
        </div>
      )}

      {room.warning_message && (
        <div style={{
          background: 'color-mix(in srgb, var(--color-warning, #7A631C) 12%, transparent)',
          color: 'var(--color-warning, #7A631C)',
          padding: '12px',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-4, 16px)',
          fontSize: '0.875rem',
        }}>
          ⚠️ {room.warning_message}
        </div>
      )}

      {room.error_message && (
        <div style={{
          background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
          color: 'var(--color-danger, #9A3D4A)',
          padding: '12px',
          borderRadius: 'var(--radius-md, 8px)',
          marginBottom: 'var(--space-4, 16px)',
          fontSize: '0.875rem',
        }}>
          ❌ {room.error_message}
        </div>
      )}

      <div style={{ display: 'flex', gap: 'var(--space-2, 8px)' }}>
        {room.status !== 'Running' && room.status !== 'Fetching' && (
          <button
            onClick={handleStart}
            style={{
              flex: 1,
              padding: '10px var(--space-4, 16px)',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              background: 'var(--color-success, #257348)',
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
              padding: '10px var(--space-4, 16px)',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              background: 'var(--color-danger, #9A3D4A)',
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
            padding: '10px var(--space-4, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-danger, #9A3D4A)',
            background: 'transparent',
            color: 'var(--color-danger, #9A3D4A)',
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
