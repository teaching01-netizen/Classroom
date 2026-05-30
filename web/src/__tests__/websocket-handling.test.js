import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSessionStore } from '../store/useSessionStore';
import { useRoomStore } from '../store/useRoomStore';

beforeEach(() => {
  useSessionStore.setState({
    sessions: [],
    currentSession: null,
    students: [],
    isLoading: false,
    error: null,
  });
});

describe('SESSION_STATS_UPDATED handling', () => {
  it('should update session stats in store when SESSION_STATS_UPDATED received', () => {
    const store = useSessionStore.getState();
    expect(typeof store.updateSessionStats).toBe('function');

    // Set a currentSession first so stats can be merged into it
    useSessionStore.setState({ currentSession: { name: 'Session 1', total_students: 0 } });
    store.updateSessionStats({
      total_students: 30,
      checked_in: 20,
      avg_attendance_rate: 0.67,
    });
    const state = useSessionStore.getState();
    expect(state.currentSession.total_students).toBe(30);
    expect(state.currentSession.checked_in).toBe(20);
    expect(state.currentSession.avg_attendance_rate).toBe(0.67);
    expect(state.currentSession.name).toBe('Session 1');
  });

  it('should not swallow SESSION_STATS_UPDATED without a handler', () => {
    const store = useSessionStore.getState();
    expect(typeof store.updateSessionStats).toBe('function');
  });
});

describe('WebSocket room updates via getState()', () => {
  it('room store getState() returns current setters', () => {
    // Verifies that getState() pattern works for accessing store actions
    const { addRoom, updateRoom, removeRoom } = useRoomStore.getState();
    expect(typeof addRoom).toBe('function');
    expect(typeof updateRoom).toBe('function');
    expect(typeof removeRoom).toBe('function');
  });
});
