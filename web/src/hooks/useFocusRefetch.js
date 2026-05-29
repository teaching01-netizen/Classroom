import { useEffect, useRef } from 'react';

export const useFocusRefetch = (callback) => {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;

  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible' && callbackRef.current) {
        callbackRef.current();
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange, false);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange, false);
    };
  }, []);
};
