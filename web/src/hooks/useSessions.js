import { useEffect, useRef, useCallback } from 'react';
import { useSessionStore } from '../store/useSessionStore';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';
import { fetchVersioned } from '../api/versionedFetch';
import { isCourseSnapshot, useSnapshotEvents } from './useSnapshotEvents';

const DEBOUNCE_MS = 150;

export const useSessions = (courseId) => {
  const { sessions, isInitialLoading, isRefreshing, error, setSessions, setCourseName, setInitialLoading, setRefreshing, setError, reset } = useSessionStore();

  const prevCourseIdRef = useRef(null);
  const displayedVersionRef = useRef(0);
  const requestedMinimumRef = useRef(0);
  const debounceTimerRef = useRef(null);

  const fetchSessions = useCallback(async ({ silent = false } = {}) => {
    if (!courseId) return;
    if (silent) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const result = await fetchVersioned(
        `/api/teacher/courses/${courseId}`,
        {},
        { displayedVersion: displayedVersionRef.current, requestedMinimumVersion: requestedMinimumRef.current }
      );
      if (!result) return; // stale response discarded
      const json = await result.response.json();
      if (json.success) {
        setSessions(json.data.sessions || []);
        setCourseName(json.data.name || '');
        if (json.snapshot?.version) {
          displayedVersionRef.current = json.snapshot.version;
        }
      } else {
        setError(json.error || 'Failed to fetch sessions');
      }
    } catch (err) {
      setError(err.message || 'Network error');
    }
  }, [courseId, setInitialLoading, setRefreshing, setSessions, setCourseName, setError]);

  useEffect(() => {
    if (prevCourseIdRef.current !== null && prevCourseIdRef.current !== courseId) {
      reset();
      displayedVersionRef.current = 0;
      requestedMinimumRef.current = 0;
    }
    prevCourseIdRef.current = courseId;

    if (!courseId) return;
    fetchSessions();
  }, [courseId, fetchSessions, reset]);

  const debouncedFetch = useCallback(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      fetchSessions({ silent: true });
    }, DEBOUNCE_MS);
  }, [fetchSessions]);

  useEffect(() => () => {
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
  }, []);

  const handleSnapshotEvent = useCallback((metadata) => {
    if (metadata?.version) {
      requestedMinimumRef.current = Math.max(requestedMinimumRef.current, metadata.version);
    }
    debouncedFetch();
  }, [debouncedFetch]);

  const silentFetch = useCallback(() => fetchSessions({ silent: true }), [fetchSessions]);
  useFocusRefetch(courseId ? silentFetch : undefined);
  useWsReconnect(courseId ? silentFetch : undefined);
  useSnapshotEvents(
    (metadata) => isCourseSnapshot(metadata, courseId),
    courseId ? handleSnapshotEvent : undefined
  );

  return { sessions, isLoading: isInitialLoading, isRefreshing, error };
};
