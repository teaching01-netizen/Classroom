import { useEffect, useRef, useCallback } from 'react';
import { useSessionStore } from '../store/useSessionStore';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';

export const useSessions = (courseId) => {
  const { sessions, isInitialLoading, isRefreshing, error, setSessions, setInitialLoading, setRefreshing, setError, reset } = useSessionStore();

  const prevCourseIdRef = useRef(null);

  const fetchSessions = useCallback(async ({ silent = false } = {}) => {
    if (!courseId) return;
    if (silent) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const res = await fetch(`/api/teacher/courses/${courseId}`);
      const result = await res.json();
      if (result.success) {
        setSessions(result.data.sessions || []);
      } else {
        setError(result.error || 'Failed to fetch sessions');
      }
    } catch (err) {
      setError(err.message || 'Network error');
    }
  }, [courseId, setInitialLoading, setRefreshing, setSessions, setError]);

  useEffect(() => {
    if (prevCourseIdRef.current !== null && prevCourseIdRef.current !== courseId) {
      reset();
    }
    prevCourseIdRef.current = courseId;

    if (!courseId) return;
    fetchSessions();
  }, [courseId, fetchSessions, reset]);

  const silentFetch = useCallback(() => fetchSessions({ silent: true }), [fetchSessions]);
  useFocusRefetch(courseId ? silentFetch : undefined);
  useWsReconnect(courseId ? silentFetch : undefined);

  return { sessions, isLoading: isInitialLoading, isRefreshing, error };
};
