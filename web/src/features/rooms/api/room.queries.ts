import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { SessionId } from '@/features/sessions'
import {
  roomSchema,
  roomsSchema,
  startSessionRoomSchema,
  type Room,
} from './room.schemas'

export const roomKeys = {
  all: ['rooms'] as const,
  detail: (roomId: string) => ['rooms', roomId] as const,
} as const

export function useRoomsQuery() {
  return useQuery({
    queryKey: roomKeys.all,
    queryFn: ({ signal }) => apiClient.get(endpoints.rooms, { schema: roomsSchema, signal }),
  })
}

export function useRoomQuery(roomId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: roomKeys.detail(roomId ?? ''),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.room(roomId ?? ''), { schema: roomSchema, signal }),
    enabled: enabled && roomId !== undefined,
    refetchInterval: (query) => {
      if (query.state.data?.qr_url) {
        return false
      }
      // Fast poll (500 ms) for the first 3 seconds while a fresh room's QR is
      // being generated, then relax to 1 s until qr_url arrives.
      const elapsed = Date.now() - query.state.dataUpdatedAt
      return elapsed < 3_000 ? 500 : 1_000
    },
  })
}

export type StartSessionRoomVariables = {
  readonly sessionId: SessionId
  readonly courseId?: string
}

// useStartRoomMutation replaces the previous create-then-start round trips
// with a single idempotent POST /api/rooms/from-session/start. The backend
// returns the full room (200) when a valid QR is already available, or
// 202 {status:'starting', room_id, retry_after_ms} while it generates one; in
// that case the room-detail query is seeded so useRoomQuery polls it.
export function useStartRoomMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ sessionId, courseId }: StartSessionRoomVariables) => {
      const result = await apiClient.post(endpoints.roomFromSessionStart, {
        body: {
          session_id: sessionId,
          ...(courseId === undefined ? {} : { course_id: courseId }),
        },
        schema: startSessionRoomSchema,
      })
      if ('qr_url' in result) {
        queryClient.setQueryData<Room>(roomKeys.detail(result.room_id), result)
        return { roomId: result.room_id, starting: false }
      }
      queryClient.setQueryData<Room>(roomKeys.detail(result.room_id), {
        room_id: result.room_id,
        status: 'starting',
      })
      return { roomId: result.room_id, starting: true }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: roomKeys.all })
    },
  })
}
