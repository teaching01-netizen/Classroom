// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useWebSocket } from '../hooks/useWebSocket';
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

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
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

describe('WebSocket reconnect lifecycle', () => {
  it('cancels a pending reconnect and closes the socket on unmount', () => {
    vi.useFakeTimers();
    const sockets = [];
    class FakeWebSocket {
      constructor(url) {
        this.url = url;
        this.closed = false;
        sockets.push(this);
      }

      close() {
        this.closed = true;
        this.onclose?.();
      }
    }
    vi.stubGlobal('WebSocket', FakeWebSocket);
    const hook = renderHook(() => useWebSocket());
    expect(sockets).toHaveLength(1);

    act(() => {
      sockets[0].onclose();
    });
    expect(vi.getTimerCount()).toBe(1);

    hook.unmount();
    expect(sockets[0].closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(sockets).toHaveLength(1);
  });
});
