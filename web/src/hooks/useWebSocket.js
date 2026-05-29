import { useEffect, useRef } from 'react';
import { useRoomStore } from '../store/useRoomStore';

const WS_URL = import.meta.env.VITE_WS_URL || 'ws://127.0.0.1:3000/ws';

export const useWebSocket = () => {
  const { setRooms, addRoom, updateRoom, removeRoom, setIsWsConnected } = useRoomStore();
  const wsRef = useRef(null);

  useEffect(() => {
    const connect = () => {
      console.log('🔌 Connecting to WebSocket at:', WS_URL);
      wsRef.current = new WebSocket(WS_URL);

      wsRef.current.onopen = () => {
        console.log('✅ WebSocket connected');
        setIsWsConnected(true);
      };

      wsRef.current.onmessage = (event) => {
        console.log('📨 Received WebSocket message:', event.data);
        try {
          const data = JSON.parse(event.data);
          console.log('📦 Parsed data:', data);
          
          if (data.FullStateSync !== undefined) {
            console.log('🔄 Setting full state sync with', data.FullStateSync.length, 'rooms');
            setRooms(data.FullStateSync);
          } else if (data.RoomCreated !== undefined) {
            console.log('➕ Room created:', data.RoomCreated);
            addRoom(data.RoomCreated);
          } else if (data.RoomUpdated !== undefined) {
            console.log('🔄 Room updated:', data.RoomUpdated);
            updateRoom(data.RoomUpdated);
          } else if (data.RoomDeleted !== undefined) {
            console.log('➖ Room deleted:', data.RoomDeleted);
            removeRoom(data.RoomDeleted);
          } else {
            console.log('⚠️ Unrecognized message format:', data);
          }
        } catch (error) {
          console.error('❌ Failed to parse WebSocket message:', error, 'Raw data:', event.data);
        }
      };

      wsRef.current.onclose = (event) => {
        console.log('🔌 WebSocket disconnected, code:', event.code, 'reason:', event.reason);
        setIsWsConnected(false);
        console.log('⏳ Reconnecting in 3 seconds...');
        setTimeout(connect, 3000);
      };

      wsRef.current.onerror = (error) => {
        console.error('❌ WebSocket error:', error);
      };
    };

    connect();

    return () => {
      if (wsRef.current) {
        console.log('🔌 Closing WebSocket connection');
        wsRef.current.close();
      }
    };
  }, [setRooms, addRoom, updateRoom, removeRoom, setIsWsConnected]);
};
