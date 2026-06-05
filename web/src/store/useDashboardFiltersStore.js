import { create } from 'zustand';

const DEFAULT_FILTERS = {
  courseIds: [],
  dateRange: null,
  threshold: 0,
  sortBy: 'risk',
  wCodes: [],
};

export const useDashboardFiltersStore = create((set, get) => ({
  filters: { ...DEFAULT_FILTERS },

  setFilters: (newFilters) => {
    set({ filters: { ...get().filters, ...newFilters } });
  },

  setCourseIds: (courseIds) => {
    set((state) => ({
      filters: { ...state.filters, courseIds },
    }));
  },

  setDateRange: (dateRange) => {
    set((state) => ({
      filters: { ...state.filters, dateRange },
    }));
  },

  setThreshold: (threshold) => {
    set((state) => ({
      filters: { ...state.filters, threshold },
    }));
  },

  setSortBy: (sortBy) => {
    set((state) => ({
      filters: { ...state.filters, sortBy },
    }));
  },

  setWCodes: (wCodes) => {
    set((state) => ({
      filters: { ...state.filters, wCodes },
    }));
  },

  loadView: (view) => {
    if (view && view.filters) {
      set({ filters: { ...view.filters } });
    }
  },

  resetFilters: () => {
    set({ filters: { ...DEFAULT_FILTERS } });
  },

  getFilterString: () => {
    const { filters } = get();
    return JSON.stringify(filters);
  },
}));

export const selectHasActiveFilters = (state) => {
  const { courseIds, dateRange, threshold, sortBy, wCodes } = state.filters;
  return (
    courseIds.length > 0 ||
    dateRange !== null ||
    threshold !== 0 ||
    sortBy !== 'risk' ||
    wCodes.length > 0
  );
};

export const selectFilterSummary = (state) => {
  const { courseIds, dateRange, threshold, sortBy, wCodes } = state.filters;
  const parts = [];
  if (courseIds.length > 0) {
    parts.push(`${courseIds.length} course${courseIds.length > 1 ? 's' : ''}`);
  }
  if (dateRange) {
    parts.push(`${dateRange.from} to ${dateRange.to}`);
  }
  if (threshold > 0) {
    parts.push(`${threshold}+ absences`);
  }
  if (sortBy !== 'risk') {
    parts.push(`sorted by ${sortBy}`);
  }
  if (wCodes.length > 0) {
    parts.push(`${wCodes.length} student${wCodes.length > 1 ? 's' : ''}`);
  }
  return parts.length > 0 ? parts.join(' · ') : 'All courses';
};
