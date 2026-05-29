import React from 'react';
import { useCountdown } from '../hooks/useCountdown';

const getStatusColor = (status) => {
  switch (status) {
    case 'Running':
      return '#4ade80';
    case 'Fetching':
      return '#fbbf24';
    case 'Warning':
      return '#f97316';
    case 'AuthExpired':
    case 'Stopped':
      return '#ef4444';
    default:
      return '#94a3b8';
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
      background: '#16213e',
      borderRadius: '12px',
      padding: '24px',
      border: '1px solid #2d3a5a',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
        <div>
          <h3 style={{ fontSize: '1.25rem', fontWeight: '600', marginBottom: '4px' }}>
            {room.name || room.class_id}
          </h3>
          <p style={{ fontSize: '0.875rem', color: '#94a3b8' }}>
            Class ID: {room.class_id}
          </p>
        </div>
        <div style={{
          padding: '4px 12px',
          borderRadius: '9999px',
          fontSize: '0.75rem',
          fontWeight: '500',
          background: `${getStatusColor(room.status)}20`,
          color: getStatusColor(room.status),
        }}>
          {room.status}
        </div>
      </div>

      {room.qr_url && (
        <div style={{ textAlign: 'center', marginBottom: '16px' }}>
          <img src={room.qr_url} alt="QR Code" style={{ width: '200px', height: '200px', borderRadius: '8px' }} />
          {timeLeft !== null && (
            <p style={{ marginTop: '8px', fontSize: '0.875rem', color: timeLeft <= 10 ? '#ef4444' : '#94a3b8' }}>
              Expires in: {timeLeft}s
            </p>
          )}
        </div>
      )}

      {room.warning_message && (
        <div style={{
          background: '#f9731620',
          color: '#f97316',
          padding: '12px',
          borderRadius: '8px',
          marginBottom: '16px',
          fontSize: '0.875rem',
        }}>
          ⚠️ {room.warning_message}
        </div>
      )}

      {room.error_message && (
        <div style={{
          background: '#ef444420',
          color: '#ef4444',
          padding: '12px',
          borderRadius: '8px',
          marginBottom: '16px',
          fontSize: '0.875rem',
        }}>
          ❌ {room.error_message}
        </div>
      )}

      <div style={{ display: 'flex', gap: '8px' }}>
        {room.status !== 'Running' && room.status !== 'Fetching' && (
          <button
            onClick={handleStart}
            style={{
              flex: 1,
              padding: '10px 16px',
              borderRadius: '8px',
              border: 'none',
              background: '#4ade80',
              color: '#000',
              fontWeight: '500',
              cursor: 'pointer',
              transition: 'background 0.2s',
            }}
            onMouseEnter={(e) => e.target.style.background = '#22c55e'}
            onMouseLeave={(e) => e.target.style.background = '#4ade80'}
          >
            Start
          </button>
        )}

        {(room.status === 'Running' || room.status === 'Fetching') && (
          <button
            onClick={handleStop}
            style={{
              flex: 1,
              padding: '10px 16px',
              borderRadius: '8px',
              border: 'none',
              background: '#ef4444',
              color: '#fff',
              fontWeight: '500',
              cursor: 'pointer',
              transition: 'background 0.2s',
            }}
            onMouseEnter={(e) => e.target.style.background = '#dc2626'}
            onMouseLeave={(e) => e.target.style.background = '#ef4444'}
          >
            Stop
          </button>
        )}

        <button
          onClick={handleDelete}
          style={{
            padding: '10px 16px',
            borderRadius: '8px',
            border: '1px solid #ef4444',
            background: 'transparent',
            color: '#ef4444',
            fontWeight: '500',
            cursor: 'pointer',
            transition: 'background 0.2s',
          }}
          onMouseEnter={(e) => e.target.style.background = '#ef444420'}
          onMouseLeave={(e) => e.target.style.background = 'transparent'}
        >
          Delete
        </button>
      </div>
    </div>
  );
};
