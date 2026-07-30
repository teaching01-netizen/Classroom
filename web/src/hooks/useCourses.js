import { useEffect, useCallback, useRef } from 'react';
import { useCourseStore } from '../store/useCourseStore';
import { useFocusRefetch } from './useFocusRefetch';
import { useWsReconnect } from './useWebSocket';
import { fetchVersioned } from '../api/versionedFetch';
import { isCatalogSnapshot, useSnapshotEvents } from './useSnapshotEvents';

const DEBOUNCE_MS = 150;

export const useCourses = () => {
  const { courses, isInitialLoading, isRefreshing, error, setCourses, setInitialLoading, setRefreshing, setError } = useCourseStore();

  const displayedVersionRef = useRef(0);
  const requestedMinimumRef = useRef(0);
  const debounceTimerRef = useRef(null);

  const fetchCourses = useCallback(async ({ silent = false } = {}) => {
    if (silent) {
      setRefreshing();
    } else {
      setInitialLoading();
    }
    try {
      const result = await fetchVersioned(
        '/api/teacher/courses',
        {},
        { displayedVersion: displayedVersionRef.current, requestedMinimumVersion: requestedMinimumRef.current }
      );
      if (!result) return;
      const json = await result.response.json();
      if (json.success) {
        setCourses(json.data.courses);
        if (json.snapshot?.version) {
          displayedVersionRef.current = json.snapshot.version;
        }
      } else {
        setError(json.error || 'Failed to fetch courses');
      }
    } catch (err) {
      setError(err.message || 'Network error');
    }
  }, [setInitialLoading, setRefreshing, setCourses, setError]);

  useEffect(() => {
    fetchCourses();
  }, [fetchCourses]);

  const debouncedFetch = useCallback(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      fetchCourses({ silent: true });
    }, DEBOUNCE_MS);
  }, [fetchCourses]);

  useEffect(() => () => {
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
  }, []);

  const handleSnapshotEvent = useCallback((metadata) => {
    if (metadata?.version) {
      requestedMinimumRef.current = Math.max(requestedMinimumRef.current, metadata.version);
    }
    debouncedFetch();
  }, [debouncedFetch]);

  const silentFetch = useCallback(() => fetchCourses({ silent: true }), [fetchCourses]);
  useFocusRefetch(silentFetch);
  useWsReconnect(silentFetch);
  useSnapshotEvents(isCatalogSnapshot, handleSnapshotEvent);

  return { courses, isLoading: isInitialLoading, isRefreshing, error };
};
