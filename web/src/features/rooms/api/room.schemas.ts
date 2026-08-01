import { z } from 'zod'
import { sessionIdSchema } from '@/features/sessions'

export const roomSchema = z.looseObject({
  room_id: z.string().min(1),
  name: z.string().nullish(),
  class_id: z.string().nullish(),
  status: z.string(),
  qr_url: z.string().nullish(),
  expires_at: z.string().nullish(),
})

export const roomsSchema = z.array(roomSchema)
export type Room = z.infer<typeof roomSchema>

// startingRoomSchema is the 202 Accepted response of the combined
// from-session/start endpoint while the QR is still being generated.
export const startingRoomSchema = z.object({
  status: z.literal('starting'),
  room_id: z.string().min(1),
  retry_after_ms: z.number().int().positive(),
})

export const startSessionRoomSchema = z.union([roomSchema, startingRoomSchema])
export type StartSessionRoomResult = z.infer<typeof startSessionRoomSchema>

export const createRoomInputSchema = z.object({
  session_id: sessionIdSchema,
})
