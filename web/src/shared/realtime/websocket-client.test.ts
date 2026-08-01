import { QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { roomKeys } from '@/features/rooms/api/room.queries'
import type { RoomSummary } from '@/features/rooms/api/room.schemas'
import { RealtimeClient } from './websocket-client'
import { useConnectionStore } from './connection-store'

// A minimal fake WebSocket that captures instances so tests can drive
// onmessage directly without a real socket or reconnect timers firing.
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((message: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor(_url: string) {
    FakeWebSocket.instances.push(this)
  }
  close() {}
}

function makeClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  const client = new RealtimeClient(queryClient)
  client.start()
  const socket = FakeWebSocket.instances.at(-1)
  if (!socket) {
    throw new Error('fake socket not created')
  }
  return { queryClient, invalidateSpy, socket }
}

function send(socket: FakeWebSocket, payload: unknown) {
  socket.onmessage?.({ data: JSON.stringify(payload) })
}

const seedRooms: RoomSummary[] = [
  { room_id: 'room-1', name: 'Room A', status: 'Idle', expires_at: null },
  { room_id: 'room-2', name: 'Room B', status: 'Running', expires_at: '2026-01-01T00:00:00Z' },
]

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useConnectionStore.getState().setStatus('offline')
})

describe('websocket room events', () => {
  it('ROOM_CHANGED patches only the matching room in the cached list', () => {
    const { queryClient, socket } = makeClient()
    queryClient.setQueryData(roomKeys.all, seedRooms)

    send(socket, {
      ROOM_CHANGED: {
        room_id: 'room-1',
        class_id: 'course-1',
        status: 'Running',
        expires_at: '2026-02-01T00:00:00Z',
        has_qr: false,
        revision: 3,
      },
    })

    expect(queryClient.getQueryData<RoomSummary[]>(roomKeys.all)).toEqual([
      { room_id: 'room-1', name: 'Room A', status: 'Running', expires_at: '2026-02-01T00:00:00Z' },
      seedRooms[1],
    ])
  })

  it('ROOM_CHANGED with has_qr=true invalidates only the exact room detail query', () => {
    const { queryClient, invalidateSpy, socket } = makeClient()
    queryClient.setQueryData(roomKeys.all, seedRooms)

    send(socket, {
      ROOM_CHANGED: { room_id: 'room-2', status: 'Running', has_qr: true, revision: 4 },
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['rooms', 'room-2'], exact: true })
    expect(invalidateSpy).not.toHaveBeenCalledWith({ queryKey: ['rooms'] })
  })

  it('ROOM_CHANGED without has_qr never invalidates the room list', () => {
    const { invalidateSpy, socket } = makeClient()

    send(socket, {
      ROOM_CHANGED: { room_id: 'room-1', status: 'Ended', has_qr: false, revision: 2 },
    })

    expect(invalidateSpy).not.toHaveBeenCalledWith({ queryKey: ['rooms'] })
    expect(invalidateSpy).not.toHaveBeenCalledWith({ queryKey: ['rooms', 'room-1'] })
  })

  it('RoomDeleted removes exactly one cached item', () => {
    const { queryClient, socket } = makeClient()
    queryClient.setQueryData(roomKeys.all, seedRooms)

    send(socket, { RoomDeleted: 'room-1' })

    expect(queryClient.getQueryData<RoomSummary[]>(roomKeys.all)).toEqual([seedRooms[1]])
  })

  it('duplicate ROOM_CHANGED revisions are ignored and higher revisions apply', () => {
    const { queryClient, invalidateSpy, socket } = makeClient()
    queryClient.setQueryData(roomKeys.all, seedRooms)

    send(socket, {
      ROOM_CHANGED: { room_id: 'room-1', status: 'Starting', has_qr: false, revision: 1 },
    })
    // Same revision again: must be a no-op.
    send(socket, {
      ROOM_CHANGED: { room_id: 'room-1', status: 'Running', has_qr: true, revision: 1 },
    })
    // Higher revision applies.
    send(socket, {
      ROOM_CHANGED: { room_id: 'room-1', status: 'Running', has_qr: true, revision: 2 },
    })

    expect(queryClient.getQueryData<RoomSummary[]>(roomKeys.all)?.[0]?.status).toBe('Running')
    expect(invalidateSpy).toHaveBeenCalledTimes(1)
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['rooms', 'room-1'], exact: true })
  })

  it('RoomCreated appends a new summary to the cached list', () => {
    const { queryClient, socket } = makeClient()
    queryClient.setQueryData(roomKeys.all, seedRooms)

    send(socket, { RoomCreated: { room_id: 'room-3', name: 'Room C', status: 'Starting' } })

    const rooms = queryClient.getQueryData<RoomSummary[]>(roomKeys.all)
    expect(rooms).toHaveLength(3)
    expect(rooms?.[2]).toEqual({ room_id: 'room-3', name: 'Room C', status: 'Starting' })
  })

  it('FullStateSync seeds the list and RoomUpdated replaces the matching room', () => {
    const { queryClient, socket } = makeClient()

    send(socket, { FullStateSync: seedRooms })
    send(socket, { RoomUpdated: { room_id: 'room-1', status: 'Running' } })

    expect(queryClient.getQueryData<RoomSummary[]>(roomKeys.all)).toEqual([
      { room_id: 'room-1', status: 'Running' },
      seedRooms[1],
    ])
  })

  it('never invalidates the complete rooms tree for any room event', () => {
    const { invalidateSpy, socket } = makeClient()

    send(socket, { ROOM_CHANGED: { room_id: 'room-1', status: 'Running', has_qr: true, revision: 1 } })
    send(socket, { RoomCreated: { room_id: 'room-9', status: 'Starting' } })
    send(socket, { RoomUpdated: { room_id: 'room-1', status: 'Running' } })
    send(socket, { RoomDeleted: 'room-9' })

    for (const call of invalidateSpy.mock.calls) {
      const queryKey = call[0]?.queryKey
      expect(Array.isArray(queryKey) && queryKey.length === 1 && queryKey[0] === 'rooms').toBe(false)
    }
  })
})
