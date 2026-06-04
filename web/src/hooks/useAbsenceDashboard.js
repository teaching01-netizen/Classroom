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

    try {
      const filterParam = encodeURIComponent(JSON.stringify(filters));
      const res = await fetch(
        `/api/teacher/absence-dashboard?filters=${filterParam}`,
        { signal: controller.signal }
      );
      const result = await res.json();
      if (result.success) {
        setData(result.data);
      } else {
        setError(result.error || 'Failed to fetch dashboard data');
      }
    } catch (err) {
      if (err.name !== 'AbortError') {
        setError(err.message || 'Network error');
      }
    } finally {
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
