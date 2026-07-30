import { useEffect, useRef, useCallback } from 'react';
import { useSessionStore } from '../store/useSessionStore';
import { usePolling } from './usePolling';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';
import { fetchFresh } from '../api/fetchFresh';
import { fetchVersioned } from '../api/versionedFetch';
import { isSessionSnapshot, useSnapshotEvents } from './useSnapshotEvents';

const POLL_INTERVAL_MS = 10000;
const DEBOUNCE_MS = 150;

export const useCheckins = (courseId, sessionId) => {
  const { students, currentSession, isInitialLoading, isRefreshing, error, setStudents, setCourseName, setCurrentSession, updateStudentCheckin, setInitialLoading, setRefreshing, setError, reset } = useSessionStore();

  const abortRef = useRef(null);
  const hasLoadedRef = useRef(false);
  const displayedVersionRef = useRef(0);
  const requestedMinimumRef = useRef(0);
  const debounceTimerRef = useRef(null);

  const fetchStudents = useCallback(async (signal) => {
    if (!courseId || !sessionId) return;
    if (hasLoadedRef.current) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const result = await fetchVersioned(
        `/api/teacher/courses/${courseId}/sessions/${sessionId}`,
        { signal },
        { displayedVersion: displayedVersionRef.current, requestedMinimumVersion: requestedMinimumRef.current }
      );
      if (!result) return;
      const json = await result.response.json();
      if (json.success) {
        setCurrentSession(json.data);
        setStudents(json.data.students || []);
        if (!hasLoadedRef.current) {
          hasLoadedRef.current = true;
        }
        if (json.snapshot?.version) {
          displayedVersionRef.current = json.snapshot.version;
        }
      } else {
        setError(json.error || 'Failed to fetch students');
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
    updateStudentCheckin(studentId, checked);
    try {
      const response = await fetchFresh(`/api/teacher/courses/${courseId}/sessions/${sessionId}/toggle-checkin`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ student_id: studentId, checked }),
      });
      const result = await response.json();
      if (!result.success) {
        updateStudentCheckin(studentId, !checked);
      } else if (result.data?.snapshot_refresh_pending) {
        fetchStudentsNoAbort();
      }
    } catch (err) {
      updateStudentCheckin(studentId, !checked);
    }
  };

  const prevKeyRef = useRef(null);

  useEffect(() => {
    const key = `${courseId}-${sessionId}`;
    if (prevKeyRef.current !== null && prevKeyRef.current !== key) {
      hasLoadedRef.current = false;
      displayedVersionRef.current = 0;
      requestedMinimumRef.current = 0;
    }
    prevKeyRef.current = key;

    abortRef.current = new AbortController();
    setInitialLoading();
    fetchStudents(abortRef.current.signal);
    return () => abortRef.current?.abort();
  }, [courseId, sessionId, fetchStudents, reset, setInitialLoading]);

  useEffect(() => {
    if (!courseId) return;
    const controller = new AbortController();
    fetchFresh(`/api/teacher/courses/${courseId}`, { signal: controller.signal })
      .then(res => res.json())
      .then(result => {
        if (result.success && result.data?.name) {
          setCourseName(result.data.name);
        }
      })
      .catch(() => {});
    return () => controller.abort();
  }, [courseId, setCourseName]);

  const debouncedFetch = useCallback(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      fetchStudents(undefined);
    }, DEBOUNCE_MS);
  }, [fetchStudents]);

  useEffect(() => () => {
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
  }, []);

  const handleSnapshotEvent = useCallback((metadata) => {
    if (metadata?.version) {
      requestedMinimumRef.current = Math.max(requestedMinimumRef.current, metadata.version);
    }
    debouncedFetch();
  }, [debouncedFetch]);

  const isActive = !!(courseId && sessionId);

  usePolling(fetchStudentsNoAbort, POLL_INTERVAL_MS, isActive);

  useFocusRefetch(isActive ? fetchStudentsNoAbort : undefined);

  useWsReconnect(isActive ? fetchStudentsNoAbort : undefined);
  useSnapshotEvents(
    (metadata) => isSessionSnapshot(metadata, courseId, sessionId),
    isActive ? handleSnapshotEvent : undefined
  );

  return { students, currentSession, isLoading: isInitialLoading, isRefreshing, error, toggleCheckin, refetch: fetchStudents };
};
