import { afterEach, describe, expect, it, vi } from 'vitest'
import { z } from 'zod'
import { apiClient } from './api-client'
import { ApiError } from './api-error'

describe('api client', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

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

  it('returns { data, snapshot } when includeSnapshot is set', async () => {
    // Given: a response carrying the snapshot envelope next to the data.
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: { count: 2 },
        snapshot: { version: 5, generatedAt: '2026-08-05T10:00:00Z', stale: true, quality: 'verified_fresh' },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    // When
    const request = apiClient.get('/api/example', {
      schema: z.object({ count: z.number().int() }),
      includeSnapshot: true,
    })
    // Then
    await expect(request).resolves.toEqual({
      data: { count: 2 },
      snapshot: { version: 5, generatedAt: '2026-08-05T10:00:00Z', stale: true, quality: 'verified_fresh' },
    })
  })

  it('resolves an undefined snapshot when includeSnapshot is set but the server sends none', async () => {
    // Given: a plain response without a snapshot envelope (live mode).
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: true, data: { count: 2 } }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    // When
    const request = apiClient.get('/api/example', {
      schema: z.object({ count: z.number().int() }),
      includeSnapshot: true,
    })
    // Then
    await expect(request).resolves.toEqual({ data: { count: 2 }, snapshot: undefined })
  })

  it('tolerates a malformed snapshot envelope without failing the request', async () => {
    // Given: an envelope whose snapshot payload does not match the schema.
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: true, data: { count: 2 }, snapshot: { version: 'nope' } }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    // When
    const request = apiClient.get('/api/example', {
      schema: z.object({ count: z.number().int() }),
      includeSnapshot: true,
    })
    // Then
    await expect(request).resolves.toEqual({ data: { count: 2 }, snapshot: undefined })
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
