import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useBatchAttendance } from '../hooks/useBatchAttendance';

const mockBatchResponse = {
  courses: {
    CS101: {
      courseId: 'CS101',
      courseName: 'Computer Science 101',
      sessions: [
        { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', status: 'done' },
      ],
      students: [
        {
          studentId: 'stu-1',
          name: 'Alice Wang',
          attendedSessions: 2,
          totalSessions: 5,
          attendanceRate: 0.4,
          atRisk: true,
          perSession: [],
        },
      ],
      errors: [],
      truncated: false,
      threshold: 1,
      computedAt: '2026-06-01T00:00:00Z',
      durationMs: 1200,
    },
    CS102: {
      courseId: 'CS102',
      courseName: 'Data Structures',
      sessions: [],
      students: [],
      errors: [],
      truncated: false,
      threshold: 0,
      computedAt: '2026-06-01T00:00:00Z',
      durationMs: 300,
    },
  },
};

describe('useBatchAttendance', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('does not fetch when courseIds is empty', async () => {
    const spy = vi.spyOn(global, 'fetch');

    const { result } = renderHook(() => useBatchAttendance([]));

    expect(result.current.loading).toBe(false);
    expect(result.current.data).toBeNull();
    expect(result.current.error).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  it('fetches batch attendance on mount when courseIds provided', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockBatchResponse }),
    });

    const { result } = renderHook(() => useBatchAttendance(['CS101', 'CS102']));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.data).toEqual(mockBatchResponse.courses);
    expect(result.current.error).toBeNull();
    expect(spy).toHaveBeenCalledTimes(1);

    const [url, options] = spy.mock.calls[0];
    expect(url).toBe('/api/teacher/courses/attendance-batch');
    expect(options.method).toBe('POST');
    expect(options.headers['Content-Type']).toBe('application/json');

    const body = JSON.parse(options.body);
    expect(body.course_ids).toEqual(['CS101', 'CS102']);
  });

  it('maps response data by course ID', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockBatchResponse }),
    });

    const { result } = renderHook(() => useBatchAttendance(['CS101', 'CS102']));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.data.CS101).toBeDefined();
    expect(result.current.data.CS101.courseName).toBe('Computer Science 101');
    expect(result.current.data.CS102).toBeDefined();
    expect(result.current.data.CS102.courseName).toBe('Data Structures');
  });

  it('handles API error', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: false, error: 'Server error' }),
    });

    const { result } = renderHook(() => useBatchAttendance(['CS101']));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('Server error');
    expect(result.current.data).toBeNull();
  });

  it('handles network error', async () => {
    vi.spyOn(global, 'fetch').mockRejectedValue(new Error('Network fail'));

    const { result } = renderHook(() => useBatchAttendance(['CS101']));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('Network fail');
  });

  it('reports HTTP errors', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
      text: async () => 'upstream error',
    });

    const { result } = renderHook(() => useBatchAttendance(['CS101']));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('HTTP 503: upstream error');
  });

  it('sends threshold when provided', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockBatchResponse }),
    });

    const { result } = renderHook(() => useBatchAttendance(['CS101'], { threshold: 3 }));

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    const body = JSON.parse(spy.mock.calls[0][1].body);
    expect(body.threshold).toBe(3);
  });

  it('refetches when courseIds change', async () => {
    const spy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: mockBatchResponse }),
    });

    const { result, rerender } = renderHook(
      ({ ids }) => useBatchAttendance(ids),
      { initialProps: { ids: ['CS101'] } }
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(spy).toHaveBeenCalledTimes(1);

    rerender({ ids: ['CS101', 'CS102'] });

    await waitFor(() => {
      expect(spy).toHaveBeenCalledTimes(2);
    });
  });
});
