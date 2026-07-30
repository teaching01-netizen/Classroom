import { afterEach, describe, expect, it, vi } from 'vitest'
import { z } from 'zod'
import { apiClient } from './api-client'
import { ApiError } from './api-error'

describe('api client', () => {
  afterEach(() => vi.restoreAllMocks())

  it('parses a successful response through the provided schema', async () => {
    // Given
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: true, data: { count: 2 } }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    // When
    const request = apiClient.get('/api/example', {
      schema: z.object({ count: z.number().int() }),
    })
    // Then
    await expect(request).resolves.toEqual({ count: 2 })
  })

  it('turns invalid JSON into a typed API error', async () => {
    // Given
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('not-json', { status: 200 }))
    // When
    const request = apiClient.get('/api/example', { schema: z.unknown() })
    // Then
    await expect(request).rejects.toMatchObject({
      name: 'ApiError',
      kind: 'invalid-json',
    } satisfies Partial<ApiError>)
  })

  it('preserves a server error message and status', async () => {
    // Given
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: false, error: 'Snapshot unavailable' }),
      { status: 503, headers: { 'Content-Type': 'application/json' } },
    ))
    // When
    const request = apiClient.get('/api/example', { schema: z.unknown() })
    // Then
    await expect(request).rejects.toMatchObject({
      name: 'ApiError',
      status: 503,
      message: 'Snapshot unavailable',
    } satisfies Partial<ApiError>)
  })
})
