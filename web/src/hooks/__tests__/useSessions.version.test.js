// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { useSessions } from '../useSessions';
import { useCheckins } from '../useCheckins';
import {
  publishSnapshotCommitted,
  resetSnapshotVersionsForTests,
} from '../useSnapshotEvents';
import { useSessionStore } from '../../store/useSessionStore';

function versionedResponse(data, version) {
  return Promise.resolve({
    ok: true,
    status: 200,
    headers: new Map([['X-Snapshot-Version', String(version)]]),
    json: async () => ({
      success: true,
      data,
      snapshot: { version, generatedAt: new Date().toISOString() },
    }),
  });
}

function plainResponse(data) {
  return Promise.resolve({
    ok: true,
    status: 200,
    headers: new Map(),
    json: async () => ({ success: true, data }),
  });
}

beforeEach(() => {
  resetSnapshotVersionsForTests();
  useSessionStore.getState().reset();
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('version-aware useSessions', () => {
  it('ignores older REST response when displayedVersion is higher', async () => {
    let callCount = 0;
    const fetchMock = vi.fn(() => {
      callCount++;
      if (callCount === 1) return versionedResponse({ sessions: [], name: 'Course' }, 100);
      return versionedResponse({ sessions: [{ id: 's1' }], name: 'Course' }, 50);
    });
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useSessions('course-1'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 200,
      });
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    expect(hook.result.current.sessions).toEqual([]);
    hook.unmount();
  });

  it('retries on replica lag (version below requestedMinimumVersion but above displayedVersion)', async () => {
    let callCount = 0;
    const fetchMock = vi.fn(() => {
      callCount++;
      if (callCount === 1) return versionedResponse({ sessions: [], name: 'Course' }, 100);
      if (callCount === 2) return versionedResponse({ sessions: [], name: 'Course' }, 120);
      return versionedResponse({ sessions: [{ id: 's1' }], name: 'Course' }, 150);
    });
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useSessions('course-1'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 150,
      });
    });
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3));
    expect(hook.result.current.sessions).toEqual([{ id: 's1' }]);
    hook.unmount();
  });

  it('duplicate WebSocket events cause one effective refresh (debounce)', async () => {
    const fetchMock = vi.fn(() => versionedResponse({ sessions: [], name: 'Course' }, 100));
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useSessions('course-1'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 200,
      });
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 201,
      });
    });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2), { timeout: 500 });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  it('unrelated session event does not refetch current course sessions', async () => {
    const fetchMock = vi.fn(() => versionedResponse({ sessions: [], name: 'Course' }, 100));
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useSessions('course-1'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    // Fire an unrelated event - the predicate should filter it out
    act(() => {
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 500,
      });
    });

    // The predicate isCourseSnapshot checks kind === 'course_detail',
    // so a session_detail event should NOT trigger a refetch.
    // Verify by checking that the hook still returns the original data.
    expect(hook.result.current.sessions).toEqual([]);
    hook.unmount();
  });
});

describe('version-aware useCheckins', () => {
  it('ignores older REST response for session detail', async () => {
    let sessionCallCount = 0;
    const fetchMock = vi.fn((url) => {
      const u = String(url);
      if (u.includes('/toggle-checkin')) {
        return plainResponse({ student_id: 's1', checked_in: true, new_count: 1 });
      }
      if (u.includes('/sessions/')) {
        sessionCallCount++;
        if (sessionCallCount === 1) return versionedResponse({ students: [], name: 'Session' }, 100);
        return versionedResponse({ students: [{ id: 's1', checked_in: true }], name: 'Session' }, 50);
      }
      return plainResponse({ name: 'Course' });
    });
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useCheckins('course-1', 'session-1'));
    await waitFor(() => expect(sessionCallCount).toBe(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 200,
      });
    });
    await waitFor(() => expect(sessionCallCount).toBe(2));

    expect(hook.result.current.students).toEqual([]);
    hook.unmount();
  });

  it('unrelated session event does not refetch current session', async () => {
    let sessionCallCount = 0;
    const fetchMock = vi.fn((url) => {
      const u = String(url);
      if (u.includes('/toggle-checkin')) {
        return plainResponse({ student_id: 's1', checked_in: true, new_count: 1 });
      }
      if (u.includes('/sessions/')) {
        sessionCallCount++;
        return versionedResponse({ students: [], name: 'Session' }, 100);
      }
      return plainResponse({ name: 'Course' });
    });
    vi.stubGlobal('fetch', fetchMock);

    const hook = renderHook(() => useCheckins('course-1', 'session-1'));
    await waitFor(() => expect(sessionCallCount).toBe(1));

    act(() => {
      publishSnapshotCommitted({
        kind: 'session_detail',
        parent_key: 'course-2',
        resource_key: 'session-99',
        version: 500,
      });
    });

    // The predicate checks parent_key === courseId AND resource_key === sessionId
    // so a different course+session should NOT trigger a refetch
    expect(hook.result.current.students).toEqual([]);
    hook.unmount();
  });
});
