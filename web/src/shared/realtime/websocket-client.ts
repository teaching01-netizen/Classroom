import type { QueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { courseIdSchema } from '@/features/courses'
import { roomKeys } from '@/features/rooms/api/room.queries'
import { sessionIdSchema, sessionKeys } from '@/features/sessions'
import {
  roomSummarySchema,
  roomSummariesSchema,
  type RoomSummary,
} from '@/features/rooms/api/room.schemas'
import { getAffectedQueryKeys } from './invalidation-map'
import {
  acceptSnapshotCommitted,
  applySnapshotStateSync,
  snapshotMetadataSchema,
} from './snapshot-events'
import { useConnectionStore } from './connection-store'
import {
  applyRoomChanged,
  applyRoomCreated,
  applyRoomDeleted,
  applyRoomUpdated,
  roomChangedSchema,
  type RoomChanged,
} from './room-events'

const checkinDeltaSchema = z.object({
  student_id: z.string(),
  checked_in: z.boolean(),
})

const checkinUpdateSchema = checkinDeltaSchema.extend({
  course_id: courseIdSchema,
  session_id: sessionIdSchema,
  observed_at: z.string().optional(),
})

const checkinsUpdateSchema = z.object({
  course_id: courseIdSchema,
  session_id: sessionIdSchema,
  observed_at: z.string().optional(),
  updates: z.array(checkinDeltaSchema),
})

const eventSchema = z.looseObject({
  FullStateSync: roomSummariesSchema.optional(),
  RoomCreated: roomSummarySchema.optional(),
  RoomUpdated: roomSummarySchema.optional(),
  RoomDeleted: z.string().optional(),
  ROOM_CHANGED: roomChangedSchema.optional(),
  CHECKIN_UPDATED: checkinUpdateSchema.optional(),
  CHECKINS_UPDATED: checkinsUpdateSchema.optional(),
  SESSION_STATS_UPDATED: z.unknown().optional(),
  SnapshotStateSync: z.array(snapshotMetadataSchema).optional(),
  SnapshotCommitted: snapshotMetadataSchema.optional(),
})

const MAX_RECONNECT_ATTEMPTS = 10
const RECONNECT_DELAY_MS = 3_000
const WS_URL = import.meta.env['VITE_WS_URL'] ?? '/ws'

export class RealtimeClient {
  readonly #queryClient: QueryClient
  #socket: WebSocket | null = null
  #reconnectTimer: number | null = null
  #reconnectAttempts = 0
  #stopped = true
  #roomRevisions = new Map<string, number>()

  constructor(queryClient: QueryClient) {
    this.#queryClient = queryClient
  }

  start(): void {
    if (!this.#stopped) {
      return
    }
    this.#stopped = false
    this.#connect()
  }

  stop(): void {
    this.#stopped = true
    if (this.#reconnectTimer !== null) {
      window.clearTimeout(this.#reconnectTimer)
      this.#reconnectTimer = null
    }
    if (this.#socket !== null) {
      this.#socket.onclose = null
      this.#socket.close()
      this.#socket = null
    }
    useConnectionStore.getState().setStatus('offline')
  }

  #connect(): void {
    if (this.#stopped) {
      return
    }
    useConnectionStore.getState().setStatus('connecting')
    const socket = new WebSocket(WS_URL)
    this.#socket = socket

    socket.onopen = () => {
      const wasReconnecting = this.#reconnectAttempts > 0
      this.#reconnectAttempts = 0
      useConnectionStore.getState().setStatus('connected')
      if (wasReconnecting) {
        useConnectionStore.getState().signalReconnect()
        void this.#queryClient.invalidateQueries()
      }
    }
    socket.onmessage = (message) => {
      this.#handleMessage(message.data)
    }
    socket.onerror = () => {
      socket.close()
    }
    socket.onclose = () => {
      useConnectionStore.getState().setStatus('offline')
      if (this.#stopped) {
        return
      }
      this.#reconnectAttempts += 1
      if (this.#reconnectAttempts <= MAX_RECONNECT_ATTEMPTS) {
        this.#reconnectTimer = window.setTimeout(() => {
          this.#reconnectTimer = null
          this.#connect()
        }, RECONNECT_DELAY_MS)
      }
    }
  }

  #applyRoomChanged(event: RoomChanged): void {
    const lastSeen = this.#roomRevisions.get(event.room_id)
    if (lastSeen !== undefined && event.revision <= lastSeen) {
      return
    }
    this.#roomRevisions.set(event.room_id, event.revision)
    // Patch only the matching room in the cached list; if the list is not
    // cached yet, FullStateSync or the list fetch will populate it.
    this.#queryClient.setQueryData<RoomSummary[]>(roomKeys.all, (rooms) =>
      applyRoomChanged(rooms, event),
    )
    if (event.has_qr) {
      // A QR became available: refetch only this room's detail query, which
      // is the sole place qr_url lives. Never refetch the whole list.
      void this.#queryClient.invalidateQueries({
        queryKey: roomKeys.detail(event.room_id),
        exact: true,
      })
    }
  }

  #handleMessage(raw: unknown): void {
    if (typeof raw !== 'string') {
      return
    }
    let decoded: unknown
    try {
      decoded = JSON.parse(raw)
    } catch {
      return
    }
    const result = eventSchema.safeParse(decoded)
    if (!result.success) {
      return
    }
    const event = result.data

    if (event.FullStateSync !== undefined) {
      // A full sync is authoritative; drop stale revision bookkeeping so a
      // server restart (or reused room id) cannot suppress fresh events.
      this.#roomRevisions.clear()
      this.#queryClient.setQueryData(roomKeys.all, event.FullStateSync)
    }
    if (event.RoomCreated !== undefined) {
      const created = event.RoomCreated
      this.#queryClient.setQueryData<RoomSummary[]>(roomKeys.all, (rooms) =>
        applyRoomCreated(rooms, created),
      )
    }
    if (event.RoomUpdated !== undefined) {
      const updated = event.RoomUpdated
      this.#queryClient.setQueryData<RoomSummary[]>(roomKeys.all, (rooms) =>
        applyRoomUpdated(rooms, updated),
      )
    }
    if (event.RoomDeleted !== undefined) {
      const deletedRoomId = event.RoomDeleted
      // Drop per-room revision bookkeeping so a reused room id (revisions
      // restart at 1) is not deduped away by the stale entry.
      this.#roomRevisions.delete(deletedRoomId)
      this.#queryClient.setQueryData<RoomSummary[]>(roomKeys.all, (rooms) =>
        applyRoomDeleted(rooms, deletedRoomId),
      )
    }
    if (event.ROOM_CHANGED !== undefined) {
      this.#applyRoomChanged(event.ROOM_CHANGED)
    }
    if (event.CHECKIN_UPDATED !== undefined) {
      const update = event.CHECKIN_UPDATED
      void this.#queryClient.invalidateQueries({
        queryKey: sessionKeys.detail(update.course_id, update.session_id),
        exact: true,
      })
    }
    if (event.CHECKINS_UPDATED !== undefined) {
      const update = event.CHECKINS_UPDATED
      void this.#queryClient.invalidateQueries({
        queryKey: sessionKeys.detail(update.course_id, update.session_id),
        exact: true,
      })
    }
    if (event.SESSION_STATS_UPDATED !== undefined) {
      void this.#queryClient.invalidateQueries({ queryKey: ['sessions'] })
    }
    if (event.SnapshotStateSync !== undefined) {
      applySnapshotStateSync(event.SnapshotStateSync)
    }
    if (event.SnapshotCommitted !== undefined && acceptSnapshotCommitted(event.SnapshotCommitted)) {
      for (const queryKey of getAffectedQueryKeys(event.SnapshotCommitted)) {
        void this.#queryClient.invalidateQueries({ queryKey })
      }
    }
  }
}
