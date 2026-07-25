// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useCheckins } from '../hooks/useCheckins';
import { useCourseAttendance } from '../hooks/useCourseAttendance';
import { useCourses } from '../hooks/useCourses';
import { useSessions } from '../hooks/useSessions';
import {
  publishSnapshotCommitted,
  resetSnapshotVersionsForTests,
} from '../hooks/useSnapshotEvents';
import { useCourseStore } from '../store/useCourseStore';
import { useSessionStore } from '../store/useSessionStore';

function response(data) {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: async () => ({ success: true, data }),
  });
}

beforeEach(() => {
  resetSnapshotVersionsForTests();
  useCourseStore.setState({
    courses: [],
    isInitialLoading: true,
    isRefreshing: false,
    error: null,
  });
  useSessionStore.getState().reset();
  vi.restoreAllMocks();
});

describe('snapshot-aware data hooks', () => {
  it('catalog commits refetch courses and unrelated commits do nothing', async () => {
    const fetchMock = vi.fn(() => response({ courses: [] }));
    vi.stubGlobal('fetch', fetchMock);
    const hook = renderHook(() => useCourses());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 1,
      });
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_catalog',
        resource_key: 'catalog',
        version: 1,
      });
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    hook.unmount();
  });

  it('course commits refetch sessions only for the open course', async () => {
    const fetchMock = vi.fn(() => response({ sessions: [], name: 'Course' }));
    vi.stubGlobal('fetch', fetchMock);
    const hook = renderHook(() => useSessions('course-1'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-2',
        version: 1,
      });
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 1,
      });
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    hook.unmount();
  });

  it('session commits refetch check-ins only for the open session', async () => {
    const fetchMock = vi.fn((url) => {
      if (String(url).endsWith('/sessions/session-1')) {
        return response({ students: [] });
      }
      return response({ name: 'Course' });
    });
    vi.stubGlobal('fetch', fetchMock);
    const hook = renderHook(() => useCheckins('course-1', 'session-1'));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });

    act(() => {
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-2',
        version: 1,
      });
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 1,
      });
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    hook.unmount();
  });

  it('session commits silently refetch an open course report', async () => {
    let resolveRefresh;
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => response({ courseId: 'course-1', students: [] }))
      .mockImplementationOnce(() => new Promise((resolve) => {
        resolveRefresh = resolve;
      }));
    vi.stubGlobal('fetch', fetchMock);
    const hook = renderHook(() => useCourseAttendance('course-1'));
    await waitFor(() => expect(hook.result.current.data?.courseId).toBe('course-1'));

    act(() => {
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 1,
      });
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(hook.result.current.data.courseId).toBe('course-1');
    expect(hook.result.current.loading).toBe(false);

    await act(async () => {
      resolveRefresh(await response({ courseId: 'course-1', students: [{ studentId: '1' }] }));
    });
    await waitFor(() => expect(hook.result.current.data.students).toHaveLength(1));
    hook.unmount();
  });
});
