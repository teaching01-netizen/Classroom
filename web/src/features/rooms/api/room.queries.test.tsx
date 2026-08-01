import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { SessionId } from '@/features/sessions'
import { roomKeys, useStartRoomMutation } from './room.queries'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderMutation(queryClient: QueryClient) {
  return renderHook(() => useStartRoomMutation(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
}

describe('useStartRoomMutation', () => {
  it('issues a single combined POST and seeds the room cache on 202', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: true, data: { status: 'starting', room_id: 'session-1', retry_after_ms: 500 } }),
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({ sessionId: 'session-1' as SessionId, courseId: 'course-1' })
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] ?? []
    expect(String(url)).toBe('/api/rooms/from-session/start')
    expect(JSON.parse(String(init?.body))).toEqual({ session_id: 'session-1', course_id: 'course-1' })
    expect(result.current.data).toEqual({ roomId: 'session-1', starting: true })
    expect(queryClient.getQueryData(roomKeys.detail('session-1'))).toEqual({
      room_id: 'session-1',
      status: 'starting',
    })
  })

  it('seeds the room cache directly when a valid QR is returned', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: { room_id: 'session-1', status: 'Running', qr_url: 'data:image/png;base64,abc' },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({ sessionId: 'session-1' as SessionId, courseId: 'course-1' })
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toEqual({ roomId: 'session-1', starting: false })
    expect(queryClient.getQueryData(roomKeys.detail('session-1'))).toMatchObject({
      room_id: 'session-1',
      qr_url: 'data:image/png;base64,abc',
    })
  })

  it('omits course_id when it is unknown', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: true, data: { status: 'starting', room_id: 'session-1', retry_after_ms: 500 } }),
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({ sessionId: 'session-1' as SessionId })
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const init = fetchMock.mock.calls[0]?.[1]
    expect(JSON.parse(String(init?.body))).toEqual({ session_id: 'session-1' })
  })
})
