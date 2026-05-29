import { create } from 'zustand';

export const useCourseStore = create((set) => ({
  courses: [],
  isInitialLoading: true,
  isRefreshing: false,
  error: null,

  setCourses: (courses) => set({ courses, isInitialLoading: false, isRefreshing: false, error: null }),
  setInitialLoading: () => set({ isInitialLoading: true, error: null }),
  setRefreshing: () => set({ isRefreshing: true, error: null }),
  setError: (error) => set({ error, isInitialLoading: false, isRefreshing: false }),
  clearError: () => set({ error: null }),
}));
