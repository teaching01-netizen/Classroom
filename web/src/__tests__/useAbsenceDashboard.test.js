import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
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

  it('does not fetch on mount', async () => {
    const spy = vi.spyOn(global, 'fetch');

    const { result } = renderHook(() => useAbsenceDashboard());

    expect(result.current.loading).toBe(false);
    expect(result.current.data).toBeNull();
    expect(result.current.error).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  it('loads data when loadDashboard is called', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockReport }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await act(async () => {
      await result.current.loadDashboard({ courseIds: [], threshold: 0, sortBy: 'risk', dateRange: null });
    });

    expect(result.current.data).toEqual(mockReport);
    expect(result.current.error).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it('handles API error via loadDashboard', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: false, error: 'Server error' }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await act(async () => {
      await result.current.loadDashboard({ courseIds: [], threshold: 0, sortBy: 'risk', dateRange: null });
    });

    expect(result.current.error).toBe('Server error');
    expect(result.current.data).toBeNull();
  });

  it('handles network error via loadDashboard', async () => {
    vi.spyOn(global, 'fetch').mockRejectedValue(new Error('Network fail'));

    const { result } = renderHook(() => useAbsenceDashboard());

    await act(async () => {
      await result.current.loadDashboard({ courseIds: [], threshold: 0, sortBy: 'risk', dateRange: null });
    });

    expect(result.current.error).toBe('Network fail');
  });

  it('encodes filters in query parameter', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockReport }),
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await act(async () => {
      await result.current.loadDashboard({ threshold: 5, courseIds: ['c1'], sortBy: 'risk', dateRange: null });
    });

    expect(spy).toHaveBeenCalledTimes(1);
    const callUrl = spy.mock.calls[0][0];
    expect(callUrl).toContain('filters=');
    const filterParam = decodeURIComponent(callUrl.split('filters=')[1]);
    const parsed = JSON.parse(filterParam);
    expect(parsed.threshold).toBe(5);
    expect(parsed.courseIds).toEqual(['c1']);
  });

  it('reports HTTP errors', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: false,
      status: 502,
      statusText: 'Bad Gateway',
      text: async () => 'upstream error',
    });

    const { result } = renderHook(() => useAbsenceDashboard());

    await act(async () => {
      await result.current.loadDashboard({ courseIds: [], threshold: 0, sortBy: 'risk', dateRange: null });
    });

    expect(result.current.error).toBe('HTTP 502: upstream error');
  });
});
