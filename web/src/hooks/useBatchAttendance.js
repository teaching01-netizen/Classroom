import { useState, useEffect, useRef } from 'react';

export function useBatchAttendance(courseIds, { threshold = 0 } = {}) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const abortRef = useRef(null);
  const idsKey = courseIds?.join(',') ?? '';

  useEffect(() => {
    if (!courseIds || courseIds.length === 0) {
      setData(null);
      setLoading(false);
      setError(null);
      return;
    }

    if (abortRef.current) {
      abortRef.current.abort();
    }
    const controller = new AbortController();
    abortRef.current = controller;

    let cancelled = false;

    async function fetchBatch() {
      setLoading(true);
      setError(null);

      try {
        const res = await fetch('/api/teacher/courses/attendance-batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ course_ids: courseIds, threshold }),
          signal: controller.signal,
        });

        if (!res.ok) {
          const text = await res.text().catch(() => res.statusText);
          throw new Error(`HTTP ${res.status}: ${text}`);
        }

        const result = await res.json();
        if (!cancelled) {
          if (result.success) {
            setData(result.data.courses ?? {});
          } else {
            setError(result.error || 'Failed to fetch batch attendance');
          }
        }
      } catch (err) {
        if (!cancelled && err.name !== 'AbortError') {
          setError(err.message || 'Network error');
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    fetchBatch();

    return () => {
      cancelled = true;
      if (abortRef.current) {
        abortRef.current.abort();
      }
    };
  }, [idsKey, threshold]);

  return { data, loading, error };
}
