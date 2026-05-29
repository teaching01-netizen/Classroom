import React, { useState } from 'react';
import { useRoomStore } from './store/useRoomStore';
import { useWebSocket } from './hooks/useWebSocket';
import { RoomCard } from './components/RoomCard';

function App() {
  const { rooms, isWsConnected } = useRoomStore();
  const [newClassId, setNewClassId] = useState('');
  const [newRoomName, setNewRoomName] = useState('');

  useWebSocket();

  const handleCreateRoom = async (e) => {
    e.preventDefault();
    if (!newClassId.trim()) return;

    try {
      const response = await fetch('/api/rooms', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          class_id: newClassId.trim(),
          name: newRoomName.trim() || undefined,
        }),
      });

      if (response.ok) {
        setNewClassId('');
        setNewRoomName('');
      }
    } catch (error) {
      console.error('Failed to create room:', error);
    }
  };

  return (
    <div style={{ minHeight: '100vh' }}>
      <header style={{
        background: '#16213e',
        borderBottom: '1px solid #2d3a5a',
        padding: '20px 32px',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
      }}>
        <div>
          <h1 style={{ fontSize: '1.75rem', fontWeight: '700' }}>Check-in QR Command Center</h1>
          <p style={{ fontSize: '0.875rem', color: '#94a3b8', marginTop: '4px' }}>
            {isWsConnected ? (
              <span style={{ color: '#4ade80' }}>● Connected</span>
            ) : (
              <span style={{ color: '#ef4444' }}>● Disconnected</span>
            )}
          </p>
        </div>
      </header>

      <main style={{ padding: '32px' }}>
        <div style={{
          background: '#16213e',
          borderRadius: '12px',
          padding: '24px',
          marginBottom: '24px',
          border: '1px solid #2d3a5a',
        }}>
          <h2 style={{ fontSize: '1.25rem', fontWeight: '600', marginBottom: '16px' }}>Create New Room</h2>
          <form onSubmit={handleCreateRoom} style={{ display: 'flex', gap: '12px', flexWrap: 'wrap' }}>
            <input
              type="text"
              placeholder="Class ID"
              value={newClassId}
              onChange={(e) => setNewClassId(e.target.value)}
              style={{
                flex: 1,
                minWidth: '200px',
                padding: '10px 16px',
                borderRadius: '8px',
                border: '1px solid #2d3a5a',
                background: '#1a1a2e',
                color: '#eee',
                fontSize: '1rem',
              }}
            />
            <input
              type="text"
              placeholder="Room Name (optional)"
              value={newRoomName}
              onChange={(e) => setNewRoomName(e.target.value)}
              style={{
                flex: 1,
                minWidth: '200px',
                padding: '10px 16px',
                borderRadius: '8px',
                border: '1px solid #2d3a5a',
                background: '#1a1a2e',
                color: '#eee',
                fontSize: '1rem',
              }}
            />
            <button
              type="submit"
              style={{
                padding: '10px 24px',
                borderRadius: '8px',
                border: 'none',
                background: '#6366f1',
                color: '#fff',
                fontWeight: '500',
                fontSize: '1rem',
                cursor: 'pointer',
                transition: 'background 0.2s',
              }}
              onMouseEnter={(e) => e.target.style.background = '#4f46e5'}
              onMouseLeave={(e) => e.target.style.background = '#6366f1'}
            >
              Create Room
            </button>
          </form>
        </div>

        <div style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(350px, 1fr))',
          gap: '24px',
        }}>
          {rooms.map((room) => (
            <RoomCard key={room.room_id} room={room} />
          ))}
        </div>

        {rooms.length === 0 && (
          <div style={{ textAlign: 'center', padding: '64px', color: '#94a3b8' }}>
            <p style={{ fontSize: '1.25rem' }}>No rooms yet. Create your first room above!</p>
          </div>
        )}
      </main>

    </div>
  );
}

export default App;
