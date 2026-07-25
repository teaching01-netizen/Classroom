// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  applySnapshotStateSync,
  isCatalogSnapshot,
  isCourseSnapshot,
  isSessionSnapshot,
  publishSnapshotCommitted,
  resetSnapshotVersionsForTests,
  SNAPSHOT_COMMITTED_EVENT,
  useSnapshotEvents,
} from '../hooks/useSnapshotEvents';

beforeEach(() => {
  resetSnapshotVersionsForTests();
});

describe('snapshot event version routing', () => {
  it('state sync initializes versions without dispatching payload events', () => {
    const listener = vi.fn();
    window.addEventListener(SNAPSHOT_COMMITTED_EVENT, listener);

    applySnapshotStateSync([{
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 4,
    }]);

    expect(listener).not.toHaveBeenCalled();
    expect(publishSnapshotCommitted({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 4,
    })).toBe(false);
    expect(publishSnapshotCommitted({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 3,
    })).toBe(false);
    expect(publishSnapshotCommitted({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 5,
    })).toBe(true);
    expect(listener).toHaveBeenCalledTimes(1);

    window.removeEventListener(SNAPSHOT_COMMITTED_EVENT, listener);
  });

  it('state sync retains the maximum version for duplicate resource metadata', () => {
    applySnapshotStateSync([
      {
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 7,
      },
      {
        kind: 'session_detail',
        parent_key: 'course-1',
        resource_key: 'session-1',
        version: 5,
      },
    ]);

    expect(publishSnapshotCommitted({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 6,
    })).toBe(false);
    expect(publishSnapshotCommitted({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 8,
    })).toBe(true);
  });

  it('routes only catalog commits to a courses refetch', () => {
    const callback = vi.fn();
    const { unmount } = renderHook(() => useSnapshotEvents(isCatalogSnapshot, callback));

    act(() => {
      publishSnapshotCommitted({
        kind: 'course_detail',
        resource_key: 'course-1',
        version: 1,
      });
      publishSnapshotCommitted({
        kind: 'course_catalog',
        resource_key: 'catalog',
        version: 1,
      });
    });

    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback.mock.calls[0][0].kind).toBe('course_catalog');
    unmount();
  });

  it('routes course and session commits only to their open resources', () => {
    const courseCallback = vi.fn();
    const checkinCallback = vi.fn();
    const reportCallback = vi.fn();
    const course = (metadata) => isCourseSnapshot(metadata, 'course-1');
    const session = (metadata) => isSessionSnapshot(metadata, 'course-1', 'session-1');
    const report = (metadata) => (
      metadata?.kind === 'session_detail' &&
      metadata?.parent_key === 'course-1'
    );
    const courseHook = renderHook(() => useSnapshotEvents(course, courseCallback));
    const checkinHook = renderHook(() => useSnapshotEvents(session, checkinCallback));
    const reportHook = renderHook(() => useSnapshotEvents(report, reportCallback));

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

    expect(courseCallback).toHaveBeenCalledTimes(1);
    expect(checkinCallback).toHaveBeenCalledTimes(1);
    expect(reportCallback).toHaveBeenCalledTimes(2);
    courseHook.unmount();
    checkinHook.unmount();
    reportHook.unmount();
  });
});
