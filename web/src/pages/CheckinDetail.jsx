import React, { useState, useMemo, useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useCheckins } from '../hooks/useCheckins';
import { StatsBar } from '../components/StatsBar';
import { StudentTable } from '../components/StudentTable';
import { QRModal } from '../components/QRModal';
import { BackBreadcrumb } from '../components/BackBreadcrumb';

export function CheckinDetail() {
  const { courseId, sessionId } = useParams();
  const { students, currentSession, isLoading, isRefreshing, error, toggleCheckin } = useCheckins(courseId, sessionId);
  const [showQR, setShowQR] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [filterStatus, setFilterStatus] = useState('all');
  const [room, setRoom] = useState(null);
  const [isStarting, setIsStarting] = useState(false);
  const pollRef = useRef(null);

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  useEffect(() => {
    fetch('/api/rooms')
      .then(r => r.json())
      .then(result => {
        if (result.success) {
          const existingRoom = result.data.find(r => r.room_id === sessionId);
          if (existingRoom) setRoom(existingRoom);
        }
      });
  }, [sessionId]);

  const filteredStudents = useMemo(() => {
    return students.filter((student) => {
      const matchesSearch = student.name.toLowerCase().includes(searchQuery.toLowerCase());
      const matchesFilter =
        filterStatus === 'all' ||
        (filterStatus === 'checked' && student.checked_in) ||
        (filterStatus === 'not-checked' && !student.checked_in);
      return matchesSearch && matchesFilter;
    });
  }, [students, searchQuery, filterStatus]);

  const stats = useMemo(() => {
    const checkedCount = students.filter((s) => s.checked_in).length;
    const totalCount = students.length;
    const rate = totalCount > 0 ? Math.round((checkedCount / totalCount) * 100) : 0;

    return [
      { value: `${checkedCount}/${totalCount}`, label: 'Checked In' },
      { value: `${rate}%`, label: 'Attendance Rate' },
    ];
  }, [students]);

  const handleExportCSV = () => {
    const headers = ['Name', 'Nickname', 'School', 'Status', 'Points'];
    const rows = students.map((s) => [
      s.name,
      s.nickname,
      s.school,
      s.checked_in ? 'Checked In' : 'Not Checked',
      s.participation_points,
    ]);

    const csv = [headers, ...rows].map((row) => row.join(',')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `checkin_${sessionId}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleStartCheckin = async () => {
    setIsStarting(true);
    try {
      const createRes = await fetch('/api/rooms/from-session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId }),
      });
      const createResult = await createRes.json();
      if (!createResult.success) {
        alert(`Failed to create room: ${createResult.error}`);
        return;
      }

      const newRoom = createResult.data;
      setRoom(newRoom);

      const startRes = await fetch(`/api/rooms/${newRoom.room_id}/start`, {
        method: 'POST',
      });
      const startResult = await startRes.json();
      if (!startResult.success) {
        alert(`Failed to start room: ${startResult.error}`);
        return;
      }

      setShowQR(true);

      pollRef.current = setInterval(async () => {
        try {
          const res = await fetch(`/api/rooms/${newRoom.room_id}`);
          const result = await res.json();
          if (result.success && result.data.qr_url) {
            setRoom(result.data);
            clearInterval(pollRef.current);
            pollRef.current = null;
          }
        } catch {
          // ignore poll errors
        }
      }, 2000);
    } catch (err) {
      alert('Failed to start check-in session');
    } finally {
      setIsStarting(false);
    }
  };

  const handleCloseQR = () => {
    setShowQR(false);
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  };

  if (isLoading) {
    return <div style={{ padding: 'var(--space-xl, 32px)', color: 'var(--text-secondary, #94a3b8)' }}>Loading students...</div>;
  }

  if (error) {
    return <div style={{ padding: 'var(--space-xl, 32px)', color: 'var(--color-danger, #ef4444)' }}>Error: {error}</div>;
  }

  return (
    <div style={{ padding: 'var(--space-xl, 32px)' }}>
      {isRefreshing && (
        <div style={{
          position: 'fixed',
          top: '12px',
          right: '12px',
          background: 'var(--bg-card, #16213e)',
          border: '1px solid var(--border-default, #2d3a5a)',
          borderRadius: 'var(--radius-md, 8px)',
          padding: '6px 12px',
          fontSize: '12px',
          color: 'var(--text-secondary, #94a3b8)',
          zIndex: 1000,
          opacity: 0.8,
        }}>
          Syncing...
        </div>
      )}
      <BackBreadcrumb to={`/courses/${courseId}/sessions`} label="Back to Sessions" />

      <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--text-primary, #fff)', marginBottom: 'var(--space-xs, 4px)' }}>
        {currentSession?.name || 'Session'}
      </h2>
      <p style={{ color: 'var(--text-secondary, #94a3b8)', marginBottom: 'var(--space-lg, 24px)' }}>Course ID: {courseId}</p>

      <StatsBar stats={stats} />

      <div
        style={{
          display: 'flex',
          gap: 'var(--space-md, 12px)',
          marginBottom: 'var(--space-lg, 24px)',
          flexWrap: 'wrap',
        }}
      >
        <button onClick={handleStartCheckin} disabled={isStarting} style={{
          padding: '10px 20px',
          borderRadius: 'var(--radius-md, 8px)',
          border: 'none',
          background: 'var(--color-success, #4ade80)',
          color: '#000',
          fontWeight: '500',
          cursor: isStarting ? 'wait' : 'pointer',
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-sm, 8px)',
        }}>
          {isStarting ? 'Starting...' : '▶ Start Check-in Session'}
        </button>

        {room && (
          <button onClick={() => setShowQR(true)} style={{
            padding: '10px 20px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'transparent',
            color: 'var(--text-secondary, #94a3b8)',
            fontWeight: '500',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-sm, 8px)',
          }}>
            📱 View QR Code
          </button>
        )}

        <button
          onClick={handleExportCSV}
          style={{
            padding: '10px 20px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'transparent',
            color: 'var(--text-secondary, #94a3b8)',
            fontWeight: '500',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-sm, 8px)',
          }}
        >
          📥 Export CSV
        </button>
      </div>

      <div
        style={{
          display: 'flex',
          gap: 'var(--space-md, 16px)',
          marginBottom: 'var(--space-lg, 24px)',
          flexWrap: 'wrap',
        }}
      >
        <input
          type="text"
          placeholder="Search students..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          style={{
            padding: '10px var(--space-md, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'var(--bg-input, #1a1a2e)',
            color: 'var(--text-primary, #eee)',
            fontSize: '14px',
            minWidth: '250px',
          }}
        />
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          style={{
            padding: '10px var(--space-md, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--border-default, #2d3a5a)',
            background: 'var(--bg-input, #1a1a2e)',
            color: 'var(--text-primary, #eee)',
            fontSize: '14px',
          }}
        >
          <option value="all">All Students</option>
          <option value="checked">Checked In</option>
          <option value="not-checked">Not Checked In</option>
        </select>
      </div>

      <StudentTable students={filteredStudents} onToggleCheckin={toggleCheckin} />

      {filteredStudents.length === 0 && (
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--text-secondary, #94a3b8)' }}>
          <p style={{ fontSize: '1.25rem' }}>
            {students.length === 0 ? 'No students enrolled' : 'No students match your search'}
          </p>
        </div>
      )}

      {showQR && (
        <QRModal
          qrUrl={room?.qr_url || currentSession?.qr_url}
          expiresIn={currentSession?.qr_expires_at}
          onClose={handleCloseQR}
        />
      )}
    </div>
  );
}
