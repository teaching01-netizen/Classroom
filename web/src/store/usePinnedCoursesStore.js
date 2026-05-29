import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

const safeStorage = {
  getItem: (name) => {
    try {
      const value = localStorage.getItem(name);
      return value ? JSON.parse(value) : null;
    } catch {
      return null;
    }
  },
  setItem: (name, value) => {
    try {
      localStorage.setItem(name, JSON.stringify(value));
    } catch (e) {
      console.warn('Failed to persist pinned courses:', e);
    }
  },
  removeItem: (name) => {
    try {
      localStorage.removeItem(name);
    } catch {
      // ignore
    }
  },
};

export const usePinnedCoursesStore = create(
  persist(
    (set, get) => ({
      pinnedCourseIds: [],

      pinCourse: (courseId) =>
        set((state) => ({
          pinnedCourseIds: state.pinnedCourseIds.includes(courseId)
            ? state.pinnedCourseIds
            : [...state.pinnedCourseIds, courseId],
        })),

      unpinCourse: (courseId) =>
        set((state) => ({
          pinnedCourseIds: state.pinnedCourseIds.filter((id) => id !== courseId),
        })),

      toggleCourse: (courseId) =>
        set((state) => ({
          pinnedCourseIds: state.pinnedCourseIds.includes(courseId)
            ? state.pinnedCourseIds.filter((id) => id !== courseId)
            : [...state.pinnedCourseIds, courseId],
        })),

      cleanupStalePins: (validCourseIds) =>
        set((state) => ({
          pinnedCourseIds: state.pinnedCourseIds.filter((id) =>
            validCourseIds.includes(id)
          ),
        })),
    }),
    {
      name: 'warwick-pinned-courses',
      version: 1,
      storage: createJSONStorage(() => safeStorage),
    }
  )
);

export const selectIsPinned = (courseId) => (state) =>
  state.pinnedCourseIds.includes(courseId);

export const selectPinnedCourses = (courses) => (state) =>
  courses.filter((c) => state.pinnedCourseIds.includes(c.course_id));

// Cross-tab sync: listen for storage changes from other tabs
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (event) => {
    if (event.key === 'warwick-pinned-courses' && event.newValue) {
      try {
        const parsed = JSON.parse(event.newValue);
        if (parsed.state && Array.isArray(parsed.state.pinnedCourseIds)) {
          usePinnedCoursesStore.setState({
            pinnedCourseIds: parsed.state.pinnedCourseIds,
          });
        }
      } catch {
        // Ignore malformed storage events
      }
    }
  });
}
