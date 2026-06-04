import { useState, useEffect, useCallback } from 'react';

const VIEWS_URL = '/api/teacher/dashboard-views';

export function useDashboardViews() {
  const [views, setViews] = useState([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchViews = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const res = await fetch(VIEWS_URL);
      const result = await res.json();
      if (result.success) {
        setViews(result.data || []);
      } else {
        setError(result.error || 'Failed to load views');
      }
    } catch (err) {
      setError(err.message || 'Network error');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchViews();
  }, [fetchViews]);

  const createView = useCallback(async (name, filters) => {
    const res = await fetch(VIEWS_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, filters }),
    });
    const result = await res.json();
    if (!res.ok || !result.success) {
      throw new Error(result.error || 'Failed to create view');
    }
    setViews((prev) => [result.data, ...prev]);
    return result.data;
  }, []);

  const updateView = useCallback(async (id, name, filters) => {
    const res = await fetch(`${VIEWS_URL}/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, filters }),
    });
    const result = await res.json();
    if (!res.ok || !result.success) {
      throw new Error(result.error || 'Failed to update view');
    }
    setViews((prev) => prev.map((v) => (v.id === id ? result.data : v)));
    return result.data;
  }, []);

  const deleteView = useCallback(async (id) => {
    const res = await fetch(`${VIEWS_URL}/${id}`, { method: 'DELETE' });
    const result = await res.json();
    if (!res.ok || !result.success) {
      throw new Error(result.error || 'Failed to delete view');
    }
    setViews((prev) => prev.filter((v) => v.id !== id));
  }, []);

  const touchView = useCallback(async (id) => {
    await fetch(`${VIEWS_URL}/${id}/use`, { method: 'POST' });
  }, []);

  return {
    views,
    isLoading,
    error,
    createView,
    updateView,
    deleteView,
    touchView,
    refetch: fetchViews,
  };
}
