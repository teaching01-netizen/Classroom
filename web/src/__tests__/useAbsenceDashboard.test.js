import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useAbsenceDashboard } from '../hooks/useAbsenceDashboard';
import { useDashboardFiltersStore } from '../store/useDashboardFiltersStore';

const mockReport = {
  generatedAt: '2026-06-01T00:00:00Z',
  totalStudents: 24,
  totalCourses: 1,
  avgAttendanceRate: 0.85,
  atRiskCount: 3,
  topAtRisk: [],
  students: [],
  sessions: [],
};

describe('useAbsenceDashboard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useDashboardFiltersStore.getState().resetFilters();
  });

  it('fetches dashboard data on mount', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockReport }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    expect(result.current.loading).toBe(true);

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.data).toEqual(mockReport);
    expect(result.current.error).toBeNull();
  });

  it('handles API error', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: false, error: 'Server error' }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('Server error');
    expect(result.current.data).toBeNull();
  });

  it('handles network error', async () => {
    vi.spyOn(global, 'fetch').mockRejectedValue(new Error('Network fail'));

    const { result } = renderHook(() => useAbsenceDashboard());

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('Network fail');
  });

  it('refetches data when filters change', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockReport }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(spy).toHaveBeenCalledTimes(1);

    useDashboardFiltersStore.getState().setThreshold(3);

    await waitFor(() => {
      expect(spy).toHaveBeenCalledTimes(2);
    });
  });

  it('encodes filters in query parameter', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockReport }),
    });

    useDashboardFiltersStore.getState().setThreshold(5);
    useDashboardFiltersStore.getState().setCourseIds(['c1']);

    renderHook(() => useAbsenceDashboard());

    await waitFor(() => {
      expect(spy).toHaveBeenCalled();
    });

    const callUrl = spy.mock.calls[0][0];
    expect(callUrl).toContain('filters=');
    const filterParam = decodeURIComponent(callUrl.split('filters=')[1]);
    const parsed = JSON.parse(filterParam);
    expect(parsed.threshold).toBe(5);
    expect(parsed.courseIds).toEqual(['c1']);
  });
});
