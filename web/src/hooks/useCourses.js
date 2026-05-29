import { useEffect, useCallback } from 'react';
import { useCourseStore } from '../store/useCourseStore';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';

export const useCourses = () => {
  const { courses, isInitialLoading, isRefreshing, error, setCourses, setInitialLoading, setRefreshing, setError } = useCourseStore();

  const fetchCourses = useCallback(async ({ silent = false } = {}) => {
    if (silent) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const res = await fetch('/api/teacher/courses');
      const result = await res.json();
      if (result.success) {
        setCourses(result.data.courses);
      } else {
        setError(result.error || 'Failed to fetch courses');
      }
    } catch (err) {
      setError(err.message || 'Network error');
    }
  }, [setInitialLoading, setRefreshing, setCourses, setError]);

  useEffect(() => {
    fetchCourses();
  }, [fetchCourses]);

  const silentFetch = useCallback(() => fetchCourses({ silent: true }), [fetchCourses]);
  useFocusRefetch(silentFetch);
  useWsReconnect(silentFetch);

  return { courses, isLoading: isInitialLoading, isRefreshing, error };
};
