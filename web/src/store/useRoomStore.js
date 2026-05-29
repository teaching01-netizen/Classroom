import { create } from 'zustand';

export const useRoomStore = create((set, get) => ({
  rooms: [],
  isWsConnected: false,
  setRooms: (rooms) => {
    console.log('🏪 Setting rooms in store:', rooms);
    set({ rooms });
  },
  addRoom: (room) => {
    console.log('🏪 Adding room to store:', room);
    set((state) => ({ rooms: [...state.rooms, room] }));
  },
  updateRoom: (updatedRoom) => {
    console.log('🏪 Updating room in store:', updatedRoom);
    set((state) => ({
      rooms: state.rooms.map((room) => {
        const currentId = String(room.room_id);
        const updatedId = String(updatedRoom.room_id);
        return currentId === updatedId ? updatedRoom : room;
      }),
    }));
  },
  removeRoom: (roomId) => {
    console.log('🏪 Removing room from store:', roomId);
    set((state) => ({
      rooms: state.rooms.filter((room) => String(room.room_id) !== String(roomId)),
    }));
  },
  setIsWsConnected: (connected) => set({ isWsConnected: connected }),
}));
