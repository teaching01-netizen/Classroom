import { useState, useEffect, useRef } from 'react';

export const useCountdown = (expiresAt) => {
  const [timeLeft, setTimeLeft] = useState(null);
  const intervalRef = useRef(null);

  useEffect(() => {
    if (!expiresAt) {
      setTimeLeft(null);
      return;
    }

    const calculateTimeLeft = () => {
      const now = new Date();
      const expiration = new Date(expiresAt);
      const diff = expiration - now;

      if (diff <= 0) {
        setTimeLeft(0);
        if (intervalRef.current) {
          clearInterval(intervalRef.current);
        }
        return;
      }

      const seconds = Math.floor(diff / 1000);
      setTimeLeft(seconds);
    };

    calculateTimeLeft();
    intervalRef.current = setInterval(calculateTimeLeft, 1000);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [expiresAt]);

  return timeLeft;
};
