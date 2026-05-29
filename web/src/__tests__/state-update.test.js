import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSessionStore } from '../store/useSessionStore';
import { useRoomStore } from '../store/useRoomStore';

// Reset stores between tests
beforeEach(() => {
  useSessionStore.setState({
    sessions: [],
    currentSession: null,
    students: [],
    isLoading: false,
    error: null,
  });
  useRoomStore.setState({
    rooms: [],
    isWsConnected: false,
  });
});

describe('useSessionStore', () => {
  it('setStudents replaces the students array', () => {
    const { setStudents } = useSessionStore.getState();
    setStudents([
      { student_id: '1', name: 'Alice', checked_in: false },
      { student_id: '2', name: 'Bob', checked_in: true },
    ]);
    const { students } = useSessionStore.getState();
    expect(students).toHaveLength(2);
    expect(students[0].name).toBe('Alice');
    expect(students[1].name).toBe('Bob');
  });

  it('updateStudentCheckin updates the correct student', () => {
    const { setStudents, updateStudentCheckin } = useSessionStore.getState();
    setStudents([
      { student_id: '1', name: 'Alice', checked_in: false },
      { student_id: '2', name: 'Bob', checked_in: false },
    ]);
    updateStudentCheckin('1', true);
    const { students } = useSessionStore.getState();
    expect(students[0].checked_in).toBe(true);
    expect(students[1].checked_in).toBe(false);
  });

  it('updateStudentCheckin does not mutate other students', () => {
    const { setStudents, updateStudentCheckin } = useSessionStore.getState();
    setStudents([
      { student_id: '1', name: 'Alice', checked_in: false },
      { student_id: '2', name: 'Bob', checked_in: false },
    ]);
    updateStudentCheckin('1', true);
    const { students } = useSessionStore.getState();
    expect(students[1]).toEqual({ student_id: '2', name: 'Bob', checked_in: false });
  });

  it('updateStudentCheckin is a no-op for unknown studentId', () => {
    const { setStudents, updateStudentCheckin } = useSessionStore.getState();
    setStudents([{ student_id: '1', name: 'Alice', checked_in: false }]);
    updateStudentCheckin('999', true);
    const { students } = useSessionStore.getState();
    expect(students).toEqual([{ student_id: '1', name: 'Alice', checked_in: false }]);
  });

  it('reset clears students, currentSession, and error', () => {
    const { setStudents, setCurrentSession, setError } = useSessionStore.getState();
    setStudents([{ student_id: '1', name: 'Alice', checked_in: false }]);
    setCurrentSession({ name: 'Session 1' });
    setError('some error');

    // reset() may not exist yet — this test will FAIL in RED phase
    const store = useSessionStore.getState();
    expect(typeof store.reset).toBe('function');
    store.reset();

    const state = useSessionStore.getState();
    expect(state.students).toEqual([]);
    expect(state.currentSession).toBeNull();
    expect(state.error).toBeNull();
  });
});

describe('useRoomStore', () => {
  it('addRoom appends a room', () => {
    const { addRoom } = useRoomStore.getState();
    addRoom({ room_id: '1', name: 'Room A' });
    addRoom({ room_id: '2', name: 'Room B' });
    const { rooms } = useRoomStore.getState();
    expect(rooms).toHaveLength(2);
    expect(rooms[0].name).toBe('Room A');
    expect(rooms[1].name).toBe('Room B');
  });

  it('updateRoom updates matching room by id', () => {
    const { setRooms, updateRoom } = useRoomStore.getState();
    setRooms([
      { room_id: '1', name: 'Room A', capacity: 10 },
      { room_id: '2', name: 'Room B', capacity: 20 },
    ]);
    updateRoom({ room_id: '1', name: 'Room A', capacity: 50 });
    const { rooms } = useRoomStore.getState();
    expect(rooms[0].capacity).toBe(50);
    expect(rooms[1].capacity).toBe(20);
  });

  it('updateRoom handles string/number id mismatch', () => {
    const { setRooms, updateRoom } = useRoomStore.getState();
    setRooms([{ room_id: '123', name: 'Room A' }]);
    updateRoom({ room_id: 123, name: 'Room A Updated' });
    const { rooms } = useRoomStore.getState();
    expect(rooms[0].name).toBe('Room A Updated');
  });

  it('removeRoom removes by id', () => {
    const { setRooms, removeRoom } = useRoomStore.getState();
    setRooms([
      { room_id: '1', name: 'Room A' },
      { room_id: '2', name: 'Room B' },
    ]);
    removeRoom('1');
    const { rooms } = useRoomStore.getState();
    expect(rooms).toHaveLength(1);
    expect(rooms[0].room_id).toBe('2');
  });
});
