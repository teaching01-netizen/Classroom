import { create } from 'zustand';

export const useRoomStore = create((set, get) => ({
  rooms: [],
  isWsConnected: false,
  lastReconnectAt: null,
  setRooms: (rooms) => set({ rooms }),
  addRoom: (room) => set((state) => ({ rooms: [...state.rooms, room] })),
  updateRoom: (updatedRoom) => {
    set((state) => ({
      rooms: state.rooms.map((room) => {
        const currentId = String(room.room_id);
        const updatedId = String(updatedRoom.room_id);
        return currentId === updatedId ? updatedRoom : room;
      }),
    }));
  },
  removeRoom: (roomId) => {
    set((state) => ({
      rooms: state.rooms.filter((room) => String(room.room_id) !== String(roomId)),
    }));
  },
  setIsWsConnected: (connected) => set({ isWsConnected: connected }),
  signalReconnect: () => set({ lastReconnectAt: Date.now() }),
}));
