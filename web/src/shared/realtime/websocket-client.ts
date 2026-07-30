import type { QueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { getAffectedQueryKeys } from './invalidation-map'
import {
  acceptSnapshotCommitted,
  applySnapshotStateSync,
  snapshotMetadataSchema,
} from './snapshot-events'
import { useConnectionStore } from './connection-store'

const checkinUpdateSchema = z.object({
  student_id: z.string(),
  checked_in: z.boolean(),
})

const eventSchema = z.looseObject({
  FullStateSync: z.unknown().optional(),
  RoomCreated: z.unknown().optional(),
  RoomUpdated: z.unknown().optional(),
  RoomDeleted: z.string().optional(),
  CHECKIN_UPDATED: checkinUpdateSchema.optional(),
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
      this.#queryClient.setQueryData(['rooms'], event.FullStateSync)
    }
    if (event.RoomCreated !== undefined || event.RoomUpdated !== undefined || event.RoomDeleted !== undefined) {
      void this.#queryClient.invalidateQueries({ queryKey: ['rooms'] })
    }
    if (event.CHECKIN_UPDATED !== undefined) {
      void this.#queryClient.invalidateQueries({ queryKey: ['sessions'] })
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
