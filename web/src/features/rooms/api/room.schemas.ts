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

export const createRoomInputSchema = z.object({
  session_id: sessionIdSchema,
})
