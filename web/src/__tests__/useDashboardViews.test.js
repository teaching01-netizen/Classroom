import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useDashboardViews } from '../hooks/useDashboardViews';

const mockViews = [
  { id: 1, name: 'View 1', filters: { courseIds: ['c1'], threshold: 3, sortBy: 'risk', dateRange: null }, lastUsedAt: '2026-06-01T00:00:00Z', createdAt: '2026-06-01T00:00:00Z', updatedAt: '2026-06-01T00:00:00Z' },
  { id: 2, name: 'View 2', filters: { courseIds: [], threshold: 0, sortBy: 'rate-asc', dateRange: null }, lastUsedAt: '2026-06-02T00:00:00Z', createdAt: '2026-06-02T00:00:00Z', updatedAt: '2026-06-02T00:00:00Z' },
];

describe('useDashboardViews', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches views on mount', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockViews }),
    });

    const { result } = renderHook(() => useDashboardViews());

    expect(result.current.isLoading).toBe(true);

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.views).toEqual(mockViews);
    expect(result.current.error).toBeNull();
  });

  it('handles fetch error', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: false, error: 'Server error' }),
    });

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.error).toBe('Server error');
    expect(result.current.views).toEqual([]);
  });

  it('handles network error', async () => {
    vi.spyOn(global, 'fetch').mockRejectedValue(new Error('Network fail'));

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.error).toBe('Network fail');
  });

  it('creates a view and prepends it to the list', async () => {
    vi.spyOn(global, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: [] }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: mockViews[0] }),
      });

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    let created;
    await act(async () => {
      created = await result.current.createView('View 1', mockViews[0].filters);
    });

    expect(created).toEqual(mockViews[0]);
    expect(result.current.views).toHaveLength(1);
    expect(result.current.views[0].name).toBe('View 1');
  });

  it('deletes a view and removes it from the list', async () => {
    vi.spyOn(global, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: mockViews }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: null }),
      });

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.views).toHaveLength(2);

    await act(async () => {
      await result.current.deleteView(1);
    });

    expect(result.current.views).toHaveLength(1);
    expect(result.current.views[0].id).toBe(2);
  });

  it('updates a view in place', async () => {
    vi.spyOn(global, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: mockViews }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          success: true,
          data: { ...mockViews[0], name: 'Updated View' },
        }),
      });

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    await act(async () => {
      await result.current.updateView(1, 'Updated View', mockViews[0].filters);
    });

    expect(result.current.views[0].name).toBe('Updated View');
  });

  it('touchView calls POST to /use endpoint', async () => {
    vi.spyOn(global, 'fetch')
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: [] }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true, data: null }),
      });

    const { result } = renderHook(() => useDashboardViews());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    await act(async () => {
      await result.current.touchView(1);
    });

    expect(global.fetch).toHaveBeenCalledWith(
      '/api/teacher/dashboard-views/1/use',
      { method: 'POST' }
    );
  });
});
