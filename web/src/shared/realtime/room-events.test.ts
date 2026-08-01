import { describe, expect, it } from 'vitest'
import type { RoomSummary } from '@/features/rooms/api/room.schemas'
import {
  applyRoomChanged,
  applyRoomCreated,
  applyRoomDeleted,
  applyRoomUpdated,
  roomChangedSchema,
} from './room-events'

const rooms: RoomSummary[] = [
  { room_id: 'room-1', name: 'Room A', status: 'Idle', expires_at: null },
  { room_id: 'room-2', name: 'Room B', status: 'Running', expires_at: '2026-01-01T00:00:00Z' },
]

describe('applyRoomChanged', () => {
  it('patches only the matching room and leaves others untouched', () => {
    const event = roomChangedSchema.parse({
      room_id: 'room-1',
      class_id: 'course-1',
      status: 'Running',
      expires_at: '2026-02-01T00:00:00Z',
      has_qr: true,
      revision: 3,
    })

    const next = applyRoomChanged(rooms, event)

    expect(next?.[0]).toEqual({
      room_id: 'room-1',
      name: 'Room A',
      status: 'Running',
      expires_at: '2026-02-01T00:00:00Z',
    })
    expect(next?.[1]).toBe(rooms[1])
    // The cached list is not mutated in place.
    expect(rooms[0]?.status).toBe('Idle')
  })

  it('leaves an uncached list undefined', () => {
    const event = roomChangedSchema.parse({
      room_id: 'room-1',
      status: 'Running',
      has_qr: false,
      revision: 1,
    })
    expect(applyRoomChanged(undefined, event)).toBeUndefined()
  })

  it('patches only status and expires_at, preserving other fields', () => {
    const event = roomChangedSchema.parse({
      room_id: 'room-2',
      status: 'Ended',
      has_qr: false,
      revision: 2,
    })
    const next = applyRoomChanged(rooms, event)
    expect(next?.[1]).toEqual({ room_id: 'room-2', name: 'Room B', status: 'Ended', expires_at: undefined })
  })
})

describe('applyRoomCreated', () => {
  it('appends a new room to the list', () => {
    const created: RoomSummary = { room_id: 'room-3', name: 'Room C', status: 'Starting' }
    const next = applyRoomCreated(rooms, created)
    expect(next).toHaveLength(3)
    expect(next[2]).toBe(created)
  })

  it('creates the list when nothing is cached', () => {
    const created: RoomSummary = { room_id: 'room-3', status: 'Starting' }
    expect(applyRoomCreated(undefined, created)).toEqual([created])
  })

  it('skips a room that already exists', () => {
    const duplicate: RoomSummary = { room_id: 'room-1', status: 'Running' }
    const next = applyRoomCreated(rooms, duplicate)
    expect(next).toHaveLength(2)
    expect(next).toBe(rooms)
  })
})

describe('applyRoomUpdated', () => {
  it('replaces the matching room with the fresh summary', () => {
    const updated: RoomSummary = { room_id: 'room-1', status: 'Running', expires_at: '2026-02-01T00:00:00Z' }
    const next = applyRoomUpdated(rooms, updated)
    expect(next[0]).toBe(updated)
    expect(next[1]).toBe(rooms[1])
  })

  it('creates the list when nothing is cached', () => {
    const updated: RoomSummary = { room_id: 'room-3', status: 'Running' }
    expect(applyRoomUpdated(undefined, updated)).toEqual([updated])
  })
})

describe('applyRoomDeleted', () => {
  it('removes the matching room and leaves others untouched', () => {
    const next = applyRoomDeleted(rooms, 'room-1')
    expect(next).toEqual([rooms[1]])
  })

  it('leaves an uncached list undefined', () => {
    expect(applyRoomDeleted(undefined, 'room-1')).toBeUndefined()
  })
})
