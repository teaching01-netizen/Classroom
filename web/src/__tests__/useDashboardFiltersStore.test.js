import { describe, it, expect, beforeEach } from 'vitest';
import { useDashboardFiltersStore, selectHasActiveFilters, selectFilterSummary } from '../store/useDashboardFiltersStore';

describe('useDashboardFiltersStore', () => {
  beforeEach(() => {
    useDashboardFiltersStore.getState().resetFilters();
  });

  it('has default filters', () => {
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.courseIds).toEqual([]);
    expect(filters.dateRange).toBeNull();
    expect(filters.threshold).toBe(0);
    expect(filters.sortBy).toBe('risk');
    expect(filters.wCodes).toEqual([]);
  });

  it('setCourseIds updates courseIds', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1', 'c2']);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.courseIds).toEqual(['c1', 'c2']);
  });

  it('setDateRange updates dateRange', () => {
    const range = { from: '2026-01-01', to: '2026-06-30' };
    useDashboardFiltersStore.getState().setDateRange(range);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.dateRange).toEqual(range);
  });

  it('setThreshold updates threshold', () => {
    useDashboardFiltersStore.getState().setThreshold(5);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.threshold).toBe(5);
  });

  it('setSortBy updates sortBy', () => {
    useDashboardFiltersStore.getState().setSortBy('rate-asc');
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.sortBy).toBe('rate-asc');
  });

  it('setWCodes updates wCodes', () => {
    useDashboardFiltersStore.getState().setWCodes(['W12345', 'W67890']);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W12345', 'W67890']);
  });

  it('setWCodes replaces wCodes array', () => {
    useDashboardFiltersStore.getState().setWCodes(['W11111']);
    useDashboardFiltersStore.getState().setWCodes(['W22222']);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.wCodes).toEqual(['W22222']);
  });

  it('setFilters merges partial filters', () => {
    useDashboardFiltersStore.getState().setFilters({ threshold: 3, sortBy: 'name' });
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.threshold).toBe(3);
    expect(filters.sortBy).toBe('name');
    expect(filters.courseIds).toEqual([]);
  });

  it('loadView replaces filters with view filters', () => {
    const view = {
      filters: {
        courseIds: ['c3'],
        dateRange: { from: '2026-03-01', to: '2026-05-01' },
        threshold: 2,
        sortBy: 'rate-desc',
        wCodes: ['W12345'],
      },
    };
    useDashboardFiltersStore.getState().loadView(view);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters).toEqual(view.filters);
  });

  it('loadView with null view does nothing', () => {
    useDashboardFiltersStore.getState().setThreshold(10);
    useDashboardFiltersStore.getState().loadView(null);
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.threshold).toBe(10);
  });

  it('resetFilters returns to defaults', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1']);
    useDashboardFiltersStore.getState().setThreshold(5);
    useDashboardFiltersStore.getState().setWCodes(['W12345']);
    useDashboardFiltersStore.getState().resetFilters();
    const { filters } = useDashboardFiltersStore.getState();
    expect(filters.courseIds).toEqual([]);
    expect(filters.threshold).toBe(0);
    expect(filters.sortBy).toBe('risk');
    expect(filters.wCodes).toEqual([]);
  });

  it('getFilterString returns JSON string of filters', () => {
    useDashboardFiltersStore.getState().setThreshold(3);
    const str = useDashboardFiltersStore.getState().getFilterString();
    const parsed = JSON.parse(str);
    expect(parsed.threshold).toBe(3);
  });
});

describe('selectHasActiveFilters', () => {
  beforeEach(() => {
    useDashboardFiltersStore.getState().resetFilters();
  });

  it('returns false when all filters are default', () => {
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(false);
  });

  it('returns true when courseIds is non-empty', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1']);
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(true);
  });

  it('returns true when dateRange is set', () => {
    useDashboardFiltersStore.getState().setDateRange({ from: '2026-01-01', to: '2026-06-30' });
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(true);
  });

  it('returns true when threshold is non-zero', () => {
    useDashboardFiltersStore.getState().setThreshold(3);
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(true);
  });

  it('returns true when sortBy is not default', () => {
    useDashboardFiltersStore.getState().setSortBy('name');
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(true);
  });

  it('returns true when wCodes is non-empty', () => {
    useDashboardFiltersStore.getState().setWCodes(['W12345']);
    expect(selectHasActiveFilters(useDashboardFiltersStore.getState())).toBe(true);
  });
});

describe('selectFilterSummary', () => {
  beforeEach(() => {
    useDashboardFiltersStore.getState().resetFilters();
  });

  it('returns "All courses" when no filters active', () => {
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('All courses');
  });

  it('shows course count', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1', 'c2']);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('2 courses');
  });

  it('shows single course', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1']);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('1 course');
  });

  it('shows threshold', () => {
    useDashboardFiltersStore.getState().setThreshold(3);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('3+ absences');
  });

  it('shows wCodes count', () => {
    useDashboardFiltersStore.getState().setWCodes(['W12345', 'W67890']);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('2 students');
  });

  it('combines multiple filters', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1']);
    useDashboardFiltersStore.getState().setThreshold(2);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('1 course · 2+ absences');
  });

  it('combines wCodes with other filters', () => {
    useDashboardFiltersStore.getState().setCourseIds(['c1']);
    useDashboardFiltersStore.getState().setWCodes(['W12345']);
    expect(selectFilterSummary(useDashboardFiltersStore.getState())).toBe('1 course · 1 student');
  });
});
