import { z } from 'zod'
import type { RoomSummary } from '@/features/rooms/api/room.schemas'

// ROOM_CHANGED carries room metadata only (never qr_url). has_qr signals that
// the QR became available, so the client refetches only the one detail query.
export const roomChangedSchema = z.object({
  room_id: z.string().min(1),
  class_id: z.string().nullish(),
  status: z.string(),
  expires_at: z.string().nullish(),
  has_qr: z.boolean(),
  revision: z.number(),
})

export type RoomChanged = z.infer<typeof roomChangedSchema>

type RoomChangedPatch = Pick<RoomChanged, 'room_id' | 'status' | 'expires_at'>

// applyRoomChanged patches only the matching room's status and expires_at.
// An undefined list means the rooms query has not been cached yet; leave it
// alone — FullStateSync or the list fetch will populate it.
export function applyRoomChanged(
  rooms: RoomSummary[] | undefined,
  event: RoomChangedPatch,
): RoomSummary[] | undefined {
  if (rooms === undefined) {
    return undefined
  }
  return rooms.map((room) =>
    room.room_id === event.room_id
      ? { ...room, status: event.status, expires_at: event.expires_at }
      : room,
  )
}

// applyRoomCreated appends a new summary, creating the list when nothing is
// cached yet. A room that already exists is left untouched.
export function applyRoomCreated(
  rooms: RoomSummary[] | undefined,
  summary: RoomSummary,
): RoomSummary[] {
  if (rooms === undefined) {
    return [summary]
  }
  return rooms.some((room) => room.room_id === summary.room_id)
    ? rooms
    : [...rooms, summary]
}

// applyRoomUpdated replaces the matching room with the fresh summary.
export function applyRoomUpdated(
  rooms: RoomSummary[] | undefined,
  summary: RoomSummary,
): RoomSummary[] {
  if (rooms === undefined) {
    return [summary]
  }
  return rooms.map((room) => (room.room_id === summary.room_id ? summary : room))
}

// applyRoomDeleted removes the matching room from the cached list.
export function applyRoomDeleted(
  rooms: RoomSummary[] | undefined,
  roomId: string,
): RoomSummary[] | undefined {
  if (rooms === undefined) {
    return undefined
  }
  return rooms.filter((room) => room.room_id !== roomId)
}
