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
  const [autoStartError, setAutoStartError] = useState(null);
  const pollRef = useRef(null);
  const startedRef = useRef(false);

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  // Auto-start QR check-in session on mount
  useEffect(() => {
    if (!sessionId) return;
    if (startedRef.current) return;
    startedRef.current = true;

    const autoStart = async () => {
      try {
        // 1. Check if room already exists
        const roomsRes = await fetch('/api/rooms');
        if (!roomsRes.ok) throw new Error(`Failed to fetch rooms: ${roomsRes.status}`);
        const roomsData = await roomsRes.json();
        if (!roomsData.success) throw new Error(roomsData.error || 'Failed to fetch rooms');
        const existingRoom = roomsData.data?.find(r => r.room_id === sessionId);

        if (existingRoom) {
          // Room exists — reuse it
          setRoom(existingRoom);
          setShowQR(true);

          // Start worker if not already running
          if (existingRoom.status !== 'Running' && existingRoom.status !== 'Fetching') {
            const startRes = await fetch(`/api/rooms/${sessionId}/start`, { method: 'POST' });
            if (!startRes.ok) throw new Error(`Failed to start room: ${startRes.status}`);
            const startResult = await startRes.json();
            if (!startResult.success) throw new Error(startResult.error || 'Failed to start room');
          }

          // Start polling for QR URL
          let pollAttempts = 0;
          const MAX_POLL_ATTEMPTS = 30;

          pollRef.current = setInterval(async () => {
            pollAttempts++;
            if (pollAttempts > MAX_POLL_ATTEMPTS) {
              clearInterval(pollRef.current);
              pollRef.current = null;
              return;
            }
            try {
              const res = await fetch(`/api/rooms/${sessionId}`);
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
        } else {
          // No room — create and start (reuse existing handler)
          await handleStartCheckin();
        }
      } catch (err) {
        console.error('Auto-start failed:', err);
        setAutoStartError('Failed to start check-in session');
      }
    };

    autoStart();
    return () => {
      startedRef.current = false;
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
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

  const checkedCount = useMemo(() => students.filter((s) => s.checked_in).length, [students]);
  const totalCount = students.length;

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
        setAutoStartError(`Failed to create room: ${createResult.error}`);
        return;
      }

      const newRoom = createResult.data;
      setRoom(newRoom);

      const startRes = await fetch(`/api/rooms/${newRoom.room_id}/start`, {
        method: 'POST',
      });
      const startResult = await startRes.json();
      if (!startResult.success) {
        setAutoStartError(`Failed to start room: ${startResult.error}`);
        return;
      }

      setShowQR(true);

      let pollAttempts = 0;
      const MAX_POLL_ATTEMPTS = 30;

      pollRef.current = setInterval(async () => {
        pollAttempts++;
        if (pollAttempts > MAX_POLL_ATTEMPTS) {
          clearInterval(pollRef.current);
          pollRef.current = null;
          return;
        }
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
      setAutoStartError('Failed to start check-in session');
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
    return <div style={{ padding: 'var(--space-8, 32px)', color: 'var(--color-text-secondary, #4F5056)' }}>Loading students...</div>;
  }

  if (error) {
    return <div style={{ padding: 'var(--space-8, 32px)', color: 'var(--color-danger, #9A3D4A)' }}>Error: {error}</div>;
  }

  if (autoStartError) {
    return (
      <div style={{ padding: 'var(--space-8, 32px)', color: 'var(--color-danger, #9A3D4A)' }}>
        Error: {autoStartError}
        <button onClick={() => { setAutoStartError(null); handleStartCheckin(); }} disabled={isStarting} style={{ marginLeft: 12 }}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <div style={{ padding: 'var(--space-8, 32px)' }}>
      {isRefreshing && (
        <div style={{
          position: 'fixed',
          top: '12px',
          right: '12px',
          background: 'var(--color-bg, #FFFFFF)',
          border: '1px solid var(--color-border, #DCDBDD)',
          borderRadius: 'var(--radius-md, 8px)',
          padding: '6px 12px',
          fontSize: '12px',
          color: 'var(--color-text-secondary, #4F5056)',
          zIndex: 1000,
          opacity: 0.8,
        }}>
          Syncing...
        </div>
      )}
      <BackBreadcrumb to={`/courses/${courseId}/sessions`} label="Back to Sessions" />

      <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--color-text-primary, #111113)', marginBottom: 'var(--space-1, 4px)' }}>
        {currentSession?.name || 'Session'}
      </h2>
      <p style={{ color: 'var(--color-text-secondary, #4F5056)', marginBottom: 'var(--space-6, 24px)' }}>Course ID: {courseId}</p>

      <StatsBar stats={stats} />

      <div
        style={{
          display: 'flex',
          gap: 'var(--space-4, 16px)',
          marginBottom: 'var(--space-6, 24px)',
          flexWrap: 'wrap',
        }}
      >
        {room && (
          <button onClick={() => setShowQR(true)} style={{
            padding: '10px 20px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'transparent',
            color: 'var(--color-text-secondary, #4F5056)',
            fontWeight: '500',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2, 8px)',
          }}>
            📱 View QR Code
          </button>
        )}

        <button
          onClick={handleExportCSV}
          style={{
            padding: '10px 20px',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'transparent',
            color: 'var(--color-text-secondary, #4F5056)',
            fontWeight: '500',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-2, 8px)',
          }}
        >
          📥 Export CSV
        </button>
      </div>

      <div
        style={{
          display: 'flex',
          gap: 'var(--space-4, 16px)',
          marginBottom: 'var(--space-6, 24px)',
          flexWrap: 'wrap',
        }}
      >
        <input
          type="text"
          placeholder="Search students..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          style={{
            padding: '10px var(--space-4, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'var(--color-bg, #FFFFFF)',
            color: 'var(--color-text-primary, #111113)',
            fontSize: '14px',
            minWidth: '250px',
          }}
        />
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          style={{
            padding: '10px var(--space-4, 16px)',
            borderRadius: 'var(--radius-md, 8px)',
            border: '1px solid var(--color-border, #DCDBDD)',
            background: 'var(--color-bg, #FFFFFF)',
            color: 'var(--color-text-primary, #111113)',
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
        <div style={{ textAlign: 'center', padding: '64px', color: 'var(--color-text-secondary, #4F5056)' }}>
          <p style={{ fontSize: '1.25rem' }}>
            {students.length === 0 ? 'No students enrolled' : 'No students match your search'}
          </p>
        </div>
      )}

      {showQR && (
        <QRModal
          qrUrl={room?.qr_url || currentSession?.qr_url}
          expiresIn={currentSession?.qr_expires_at || room?.expires_at}
          onClose={handleCloseQR}
          courseId={courseId}
          roomName={room?.name}
          className={currentSession?.name}
          checkedCount={checkedCount}
          totalCount={totalCount}
          onRefresh={handleStartCheckin}
        />
      )}
    </div>
  );
}
