import { create } from 'zustand';

export const useSessionStore = create((set) => ({
  sessions: [],
  currentSession: null,
  students: [],
  isInitialLoading: true,
  isRefreshing: false,
  error: null,

  setSessions: (sessions) => set({ sessions, isInitialLoading: false, isRefreshing: false, error: null }),
  setCurrentSession: (session) => set({ currentSession: session }),
  setStudents: (students) => set({ students, isInitialLoading: false, isRefreshing: false, error: null }),
  updateStudentCheckin: (studentId, checkedIn) => set((state) => ({
    students: state.students.map(s =>
      s.student_id === studentId ? { ...s, checked_in: checkedIn } : s
    )
  })),
  updateSessionStats: (stats) => set((state) => ({
    currentSession: state.currentSession ? { ...state.currentSession, ...stats } : null,
  })),
  setInitialLoading: () => set({ isInitialLoading: true, error: null }),
  setRefreshing: () => set({ isRefreshing: true, error: null }),
  setError: (error) => set({ error, isInitialLoading: false, isRefreshing: false }),
  clearError: () => set({ error: null }),
  reset: () => set({
    students: [],
    currentSession: null,
    isInitialLoading: true,
    isRefreshing: false,
    error: null,
  }),
}));
