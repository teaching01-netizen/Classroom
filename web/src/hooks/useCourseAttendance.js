import { useState, useEffect, useCallback, useRef } from 'react';
import { fetchFresh } from '../api/fetchFresh';
import {
  isCourseSessionSnapshot,
  useSnapshotEvents,
} from './useSnapshotEvents';

export function useCourseAttendance(courseId, { threshold = 0 } = {}) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const abortRef = useRef(null);

  const fetchData = useCallback(async ({ silent = false } = {}) => {
    if (!courseId) return;

    if (abortRef.current) {
      abortRef.current.abort();
    }
    const controller = new AbortController();
    abortRef.current = controller;

    if (!silent) {
      setLoading(true);
    }
    setError(null);

    try {
      const res = await fetchFresh(
        `/api/teacher/courses/${courseId}/attendance-report?threshold=${threshold}`,
        { signal: controller.signal }
      );
      const result = await res.json();
      if (result.success) {
        setData(result.data);
      } else {
        setError(result.error || 'Failed to fetch attendance report');
      }
    } catch (err) {
      if (err.name !== 'AbortError') {
        setError(err.message || 'Network error');
      }
    } finally {
      setLoading(false);
    }
  }, [courseId, threshold]);

  useEffect(() => {
    fetchData();
    return () => {
      if (abortRef.current) {
        abortRef.current.abort();
      }
    };
  }, [fetchData]);

  const refetch = useCallback(() => {
    fetchData();
  }, [fetchData]);
  const silentRefetch = useCallback(() => {
    fetchData({ silent: true });
  }, [fetchData]);
  useSnapshotEvents(
    (metadata) => isCourseSessionSnapshot(metadata, courseId),
    courseId ? silentRefetch : undefined
  );

  return {
    data,
    loading,
    error,
    refetch,
    truncated: data?.truncated ?? false,
    errors: data?.errors ?? [],
  };
}
