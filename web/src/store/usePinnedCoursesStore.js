import { create } from 'zustand';

const FAVOURITES_URL = '/api/teacher/favourites';

export const usePinnedCoursesStore = create((set, get) => ({
  pinnedCourseIds: [],
  isLoading: false,

  loadFavourites: async () => {
    set({ isLoading: true });
    try {
      const res = await fetch(FAVOURITES_URL);
      const result = await res.json();
      if (result.success) {
        set({ pinnedCourseIds: result.data.favourite_ids, isLoading: false });
      } else {
        set({ isLoading: false });
      }
    } catch (err) {
      console.error('Failed to load favourites:', err);
      set({ isLoading: false });
    }
  },

  pinCourse: async (courseId) => {
    set({ isLoading: true });
    try {
      const res = await fetch(FAVOURITES_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ course_id: courseId }),
      });
      if (!res.ok) {
        throw new Error(`Pin failed: ${res.status}`);
      }
      set((state) => ({
        pinnedCourseIds: state.pinnedCourseIds.includes(courseId)
          ? state.pinnedCourseIds
          : [...state.pinnedCourseIds, courseId],
        isLoading: false,
      }));
    } catch (err) {
      console.error('Failed to pin course:', err);
      set({ isLoading: false });
    }
  },

  unpinCourse: async (courseId) => {
    set({ isLoading: true });
    try {
      const res = await fetch(`${FAVOURITES_URL}/${courseId}`, { method: 'DELETE' });
      if (!res.ok) {
        throw new Error(`Unpin failed: ${res.status}`);
      }
      set((state) => ({
        pinnedCourseIds: state.pinnedCourseIds.filter((id) => id !== courseId),
        isLoading: false,
      }));
    } catch (err) {
      console.error('Failed to unpin course:', err);
      set({ isLoading: false });
    }
  },

  toggleCourse: async (courseId) => {
    const { pinnedCourseIds } = get();
    if (pinnedCourseIds.includes(courseId)) {
      await get().unpinCourse(courseId);
    } else {
      await get().pinCourse(courseId);
    }
  },
}));

export const selectIsPinned = (courseId) => (state) =>
  state.pinnedCourseIds.includes(courseId);

export const selectPinnedCourses = (courses) => (state) =>
  courses.filter((c) => state.pinnedCourseIds.includes(c.course_id));
