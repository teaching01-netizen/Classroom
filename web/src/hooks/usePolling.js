import { useEffect, useRef } from 'react';

export const usePolling = (callback, intervalMs, enabled = true, immediate = false) => {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;

  useEffect(() => {
    if (!enabled || !intervalMs || intervalMs <= 0) return;

    if (immediate) {
      callbackRef.current();
    }

    const id = setInterval(() => {
      callbackRef.current();
    }, intervalMs);

    return () => clearInterval(id);
  }, [intervalMs, enabled, immediate]);
};
