import { useState, useCallback, useRef } from 'react';

export function useAbsenceDashboard() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const abortRef = useRef(null);

  const loadDashboard = useCallback(async (filters) => {
    if (abortRef.current) {
      abortRef.current.abort();
    }
    const controller = new AbortController();
    abortRef.current = controller;

    setLoading(true);
    setError(null);

    const timeout = setTimeout(() => {
      console.error('[Dashboard] Request timed out after 90s');
      controller.abort();
    }, 90000);

    try {
      const filterParam = encodeURIComponent(JSON.stringify(filters));
      console.log('[Dashboard] Loading dashboard...', { filters });
      const res = await fetch(
        `/api/teacher/absence-dashboard?filters=${filterParam}`,
        { signal: controller.signal }
      );
      console.log('[Dashboard] Response status:', res.status);
      if (!res.ok) {
        const text = await res.text().catch(() => '');
        console.error('[Dashboard] HTTP error:', res.status, text);
        setError(`HTTP ${res.status}: ${text || res.statusText}`);
        return;
      }
      const result = await res.json();
      console.log('[Dashboard] Result:', { success: result.success, error: result.error });
      if (result.success) {
        setData(result.data);
      } else {
        setError(result.error || 'Failed to fetch dashboard data');
      }
    } catch (err) {
      if (err.name === 'AbortError') {
        setError('Request timed out (90s). The server may be overloaded — check backend logs for pool exhaustion.');
      } else {
        console.error('[Dashboard] Fetch error:', err);
        setError(err.message || 'Network error');
      }
    } finally {
      clearTimeout(timeout);
      setLoading(false);
    }
  }, []);

  const cancel = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort();
    }
    setLoading(false);
  }, []);

  return {
    data,
    loading,
    error,
    loadDashboard,
    cancel,
  };
}
