import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import type { SessionId } from '@/features/sessions'
import { roomSchema, roomsSchema } from './room.schemas'

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
    refetchInterval: (query) => query.state.data?.qr_url ? false : 2_000,
  })
}

export function useStartRoomMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ sessionId, roomId }: {
      readonly sessionId: SessionId
      readonly roomId?: string
    }) => {
      const room = roomId === undefined
        ? await apiClient.post(endpoints.roomFromSession, {
            body: { session_id: sessionId },
            schema: roomSchema,
          })
        : await apiClient.get(endpoints.room(roomId), { schema: roomSchema })
      await apiClient.post(endpoints.startRoom(room.room_id), {
        schema: z.null(),
      })
      return room
    },
    onSuccess: (room) => {
      queryClient.setQueryData(roomKeys.detail(room.room_id), room)
      void queryClient.invalidateQueries({ queryKey: roomKeys.all })
    },
  })
}
