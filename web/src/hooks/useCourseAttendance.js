import { useState, useEffect, useCallback, useRef } from 'react';

export function useCourseAttendance(courseId, { threshold = 0 } = {}) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const abortRef = useRef(null);

  const fetchData = useCallback(async () => {
    if (!courseId) return;

    if (abortRef.current) {
      abortRef.current.abort();
    }
    const controller = new AbortController();
    abortRef.current = controller;

    setLoading(true);
    setError(null);

    try {
      const res = await fetch(
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

  return {
    data,
    loading,
    error,
    refetch,
    truncated: data?.truncated ?? false,
    errors: data?.errors ?? [],
  };
}
