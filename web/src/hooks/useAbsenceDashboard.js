import { useState, useEffect, useCallback, useRef } from 'react';
import { useDashboardFiltersStore } from '../store/useDashboardFiltersStore';

export function useAbsenceDashboard() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const abortRef = useRef(null);
  const filters = useDashboardFiltersStore((s) => s.filters);

  const fetchData = useCallback(async () => {
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
      console.log('[Dashboard] Fetching absence dashboard...', { filters, url: `/api/teacher/absence-dashboard?filters=${filterParam}` });
      const res = await fetch(
        `/api/teacher/absence-dashboard?filters=${filterParam}`,
        { signal: controller.signal }
      );
      console.log('[Dashboard] Response status:', res.status, 'ok:', res.ok);
      if (!res.ok) {
        const text = await res.text().catch(() => '');
        console.error('[Dashboard] HTTP error:', res.status, text);
        setError(`HTTP ${res.status}: ${text || res.statusText}`);
        return;
      }
      const result = await res.json();
      console.log('[Dashboard] Result:', { success: result.success, error: result.error, dataKeys: result.data ? Object.keys(result.data) : null });
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
  }, [filters]);

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
  };
}
