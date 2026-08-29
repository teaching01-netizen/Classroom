import { z } from 'zod'
import { sessionIdSchema } from '@/features/sessions'

// roomSummarySchema is the lite wire shape: room lists and room events carry
// no QR payload. z.object (strip mode) drops any qr_url that leaks through.
export const roomSummarySchema = z.object({
  room_id: z.string().min(1),
  class_id: z.string().nullish(),
  name: z.string().nullish(),
  status: z.string(),
  expires_at: z.string().nullish(),
})

export const roomSummariesSchema = z.array(roomSummarySchema)
export type RoomSummary = z.infer<typeof roomSummarySchema>

// roomDetailSchema is the QR-bearing detail response; only the detail query
// may see qr_url.
export const roomDetailSchema = roomSummarySchema.extend({
  qr_url: z.string().nullish(),
  warning_message: z.string().nullish(),
  error_message: z.string().nullish(),
  upstream_attendance_label: z.string().nullish(),
  upstream_verified_at: z.string().nullish(),
  upstream_verification_error: z.string().nullish(),
})
export type RoomDetail = z.infer<typeof roomDetailSchema>

// startingRoomSchema is the 202 Accepted response of the combined
// from-session/start endpoint while the QR is still being generated.
export const startingRoomSchema = z.object({
  status: z.literal('starting'),
  room_id: z.string().min(1),
  retry_after_ms: z.number().int().positive(),
})

export const startSessionRoomSchema = z.union([roomDetailSchema, startingRoomSchema])
export type StartSessionRoomResult = z.infer<typeof startSessionRoomSchema>

export const createRoomInputSchema = z.object({
  session_id: sessionIdSchema,
})
