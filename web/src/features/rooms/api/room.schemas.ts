import { z } from 'zod'
import { sessionIdSchema } from '@/features/sessions'

export const roomSchema = z.looseObject({
  room_id: z.string().min(1),
  name: z.string().optional(),
  class_id: z.string().optional(),
  status: z.string(),
  qr_url: z.string().optional(),
  expires_at: z.string().optional(),
})

export const roomsSchema = z.array(roomSchema)
export type Room = z.infer<typeof roomSchema>

export const createRoomInputSchema = z.object({
  session_id: sessionIdSchema,
})
