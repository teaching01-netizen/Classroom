import { describe, expect, it } from 'vitest'
import {
  roomDetailSchema,
  roomSummariesSchema,
  roomSummarySchema,
} from './room.schemas'

describe('roomSummarySchema', () => {
  it('parses a payload without qr_url', () => {
    const result = roomSummarySchema.parse({
      room_id: 'room-1',
      class_id: 'class-1',
      name: 'Room A',
      status: 'Running',
      expires_at: '2026-01-01T00:00:00Z',
    })
    expect(result).toEqual({
      room_id: 'room-1',
      class_id: 'class-1',
      name: 'Room A',
      status: 'Running',
      expires_at: '2026-01-01T00:00:00Z',
    })
  })

  it('strips qr_url / warning_message / error_message when present in input', () => {
    const result = roomSummarySchema.parse({
      room_id: 'room-1',
      status: 'Running',
      qr_url: 'data:image/png;base64,abc',
      warning_message: 'warn',
      error_message: 'err',
    })
    expect(result).not.toHaveProperty('qr_url')
    expect(result).not.toHaveProperty('warning_message')
    expect(result).not.toHaveProperty('error_message')
    expect('qr_url' in roomSummarySchema.shape).toBe(false)
    expect('warning_message' in roomSummarySchema.shape).toBe(false)
    expect('error_message' in roomSummarySchema.shape).toBe(false)
  })
})

describe('roomDetailSchema', () => {
  it('parses with qr_url and warning/error messages', () => {
    const result = roomDetailSchema.parse({
      room_id: 'room-1',
      status: 'Running',
      qr_url: 'data:image/png;base64,abc',
      warning_message: 'warn',
      error_message: 'err',
    })
    expect(result).toEqual({
      room_id: 'room-1',
      status: 'Running',
      qr_url: 'data:image/png;base64,abc',
      warning_message: 'warn',
      error_message: 'err',
    })
  })
})

describe('roomSummariesSchema', () => {
  it('parses an array of room summaries', () => {
    const result = roomSummariesSchema.parse([
      { room_id: 'room-1', status: 'Running' },
      { room_id: 'room-2', status: 'Ended', name: 'Room B' },
    ])
    expect(result).toHaveLength(2)
    expect(result[0]).toMatchObject({ room_id: 'room-1', status: 'Running' })
    expect(result[1]).toMatchObject({ room_id: 'room-2', status: 'Ended', name: 'Room B' })
  })
})
