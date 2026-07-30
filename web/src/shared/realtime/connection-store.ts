import { create } from 'zustand'

export type ConnectionStatus = 'connecting' | 'connected' | 'offline'

type ConnectionState = {
  readonly status: ConnectionStatus
  readonly reconnectCount: number
  readonly setStatus: (status: ConnectionStatus) => void
  readonly signalReconnect: () => void
}

export const useConnectionStore = create<ConnectionState>((set) => ({
  status: 'connecting',
  reconnectCount: 0,
  setStatus: (status) => set({ status }),
  signalReconnect: () => set((state) => ({ reconnectCount: state.reconnectCount + 1 })),
}))

export function isRealtimeConnected(): boolean {
  return useConnectionStore.getState().status === 'connected'
}
