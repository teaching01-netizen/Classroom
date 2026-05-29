import { create } from 'zustand';
import { persist } from 'zustand/middleware';

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

      toggleCourse: (courseId) => {
        const { pinnedCourseIds } = get();
        if (pinnedCourseIds.includes(courseId)) {
          set({ pinnedCourseIds: pinnedCourseIds.filter((id) => id !== courseId) });
        } else {
          set({ pinnedCourseIds: [...pinnedCourseIds, courseId] });
        }
      },

      isPinned: (courseId) => get().pinnedCourseIds.includes(courseId),

      getPinnedCourses: (courses) => {
        const { pinnedCourseIds } = get();
        return courses.filter((course) => pinnedCourseIds.includes(course.course_id));
      },

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
    }
  )
);

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
