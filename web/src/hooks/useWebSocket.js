import { useEffect, useRef } from 'react';
import { useRoomStore } from '../store/useRoomStore';
import { useSessionStore } from '../store/useSessionStore';

const WS_URL = import.meta.env.VITE_WS_URL || `/ws`;

export const WS_RECONNECT_EVENT = 'ws-reconnect';

export const useWebSocket = () => {
  const wsRef = useRef(null);
  const reconnectAttempts = useRef(0);
  const MAX_RECONNECT = 10;

  useEffect(() => {
    const connect = () => {
      wsRef.current = new WebSocket(WS_URL);

      wsRef.current.onopen = () => {
        const wasReconnecting = reconnectAttempts.current > 0;
        reconnectAttempts.current = 0;
        useRoomStore.getState().setIsWsConnected(true);
        if (wasReconnecting) {
          useRoomStore.getState().signalReconnect();
          window.dispatchEvent(new CustomEvent(WS_RECONNECT_EVENT));
        }
      };

      wsRef.current.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          const roomActions = useRoomStore.getState();
          const sessionActions = useSessionStore.getState();

          if (data.FullStateSync !== undefined) {
            roomActions.setRooms(data.FullStateSync);
          } else if (data.RoomCreated !== undefined) {
            roomActions.addRoom(data.RoomCreated);
          } else if (data.RoomUpdated !== undefined) {
            roomActions.updateRoom(data.RoomUpdated);
          } else if (data.RoomDeleted !== undefined) {
            roomActions.removeRoom(data.RoomDeleted);
          } else if (data.CHECKIN_UPDATED !== undefined) {
            sessionActions.updateStudentCheckin(data.CHECKIN_UPDATED.student_id, data.CHECKIN_UPDATED.checked_in);
          } else if (data.SESSION_STATS_UPDATED !== undefined) {
            sessionActions.updateSessionStats(data.SESSION_STATS_UPDATED);
          }
        } catch (error) {
          console.error('WebSocket message parse error:', error);
        }
      };

      wsRef.current.onclose = (event) => {
        useRoomStore.getState().setIsWsConnected(false);
        reconnectAttempts.current += 1;
        if (reconnectAttempts.current <= MAX_RECONNECT) {
          setTimeout(connect, 3000);
        }
      };

      wsRef.current.onerror = () => {};
    };

    connect();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);
};

export const useWsReconnect = (callback) => {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;

  useEffect(() => {
    if (!callback) return;
    const handler = () => callbackRef.current?.();
    window.addEventListener(WS_RECONNECT_EVENT, handler);
    return () => window.removeEventListener(WS_RECONNECT_EVENT, handler);
  }, [callback]);
};
