import { useEffect, useRef, useCallback } from 'react';
import { useSessionStore } from '../store/useSessionStore';
import { usePolling } from './usePolling';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';

const POLL_INTERVAL_MS = 10000;

export const useCheckins = (courseId, sessionId) => {
  const { students, currentSession, isInitialLoading, isRefreshing, error, setStudents, setCurrentSession, updateStudentCheckin, setInitialLoading, setRefreshing, setError, reset } = useSessionStore();

  const abortRef = useRef(null);
  const hasLoadedRef = useRef(false);

  const fetchStudents = useCallback(async (signal) => {
    if (!courseId || !sessionId) return;
    if (hasLoadedRef.current) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const response = await fetch(`/api/teacher/courses/${courseId}/sessions/${sessionId}`, { signal });
      const result = await response.json();
      if (result.success) {
        setCurrentSession(result.data);
        setStudents(result.data.students || []);
        if (!hasLoadedRef.current) {
          hasLoadedRef.current = true;
        }
      } else {
        setError(result.error || 'Failed to fetch students');
      }
    } catch (err) {
      if (err.name !== 'AbortError') {
        setError(err.message || 'Network error');
      }
    }
  }, [courseId, sessionId, setInitialLoading, setRefreshing, setCurrentSession, setStudents, setError]);

  const fetchStudentsNoAbort = useCallback(() => {
    fetchStudents(undefined);
  }, [fetchStudents]);

  const toggleCheckin = async (studentId, checked) => {
    try {
      const response = await fetch(`/api/teacher/courses/${courseId}/sessions/${sessionId}/toggle-checkin`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ student_id: studentId, checked }),
      });
      const result = await response.json();
      if (result.success) {
        updateStudentCheckin(studentId, checked);
      }
    } catch (err) {
      console.error('Failed to toggle checkin:', err);
    }
  };

  const prevKeyRef = useRef(null);

  useEffect(() => {
    const key = `${courseId}-${sessionId}`;
    if (prevKeyRef.current !== null && prevKeyRef.current !== key) {
      reset();
      hasLoadedRef.current = false;
    }
    prevKeyRef.current = key;

    abortRef.current = new AbortController();
    setInitialLoading();
    fetchStudents(abortRef.current.signal);
    return () => abortRef.current?.abort();
  }, [courseId, sessionId, fetchStudents, reset, setInitialLoading]);

  const isActive = !!(courseId && sessionId);

  usePolling(fetchStudentsNoAbort, POLL_INTERVAL_MS, isActive);

  useFocusRefetch(isActive ? fetchStudentsNoAbort : undefined);

  useWsReconnect(isActive ? fetchStudentsNoAbort : undefined);

  return { students, currentSession, isLoading: isInitialLoading, isRefreshing, error, toggleCheckin, refetch: fetchStudents };
};
