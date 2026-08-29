import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SessionId } from '@/features/sessions'
import { roomKeys, useRoomQuery, useRoomsQuery, useStartRoomMutation } from './room.queries'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderQuery<TResult>(hook: () => TResult, queryClient: QueryClient) {
  return renderHook(hook, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
}

function renderMutation(queryClient: QueryClient) {
  return renderQuery(() => useStartRoomMutation(), queryClient)
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

describe('useRoomsQuery', () => {
  it('parses the list with the summary schema and strips qr_url', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: [
          { room_id: 'room-1', status: 'Running', qr_url: 'data:image/png;base64,abc', name: 'Room A' },
          { room_id: 'room-2', status: 'Ended', qr_url: 'data:image/png;base64,def' },
        ],
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderQuery(() => useRoomsQuery(), queryClient)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetchMock).toHaveBeenCalledWith('/api/rooms?lite=true', expect.anything())
    const rooms = result.current.data
    expect(rooms).toHaveLength(2)
    for (const room of rooms ?? []) {
      expect(room).not.toHaveProperty('qr_url')
    }
  })
})

describe('useRoomQuery', () => {
  it('parses qr_url from the detail response', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: {
          room_id: 'room-1',
          status: 'Running',
          qr_url: 'data:image/png;base64,abc',
          warning_message: 'expiring soon',
        },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderQuery(() => useRoomQuery('room-1', true), queryClient)

    await waitFor(() => expect(result.current.data?.qr_url).toBe('data:image/png;base64,abc'))

    expect(fetchMock).toHaveBeenCalledWith('/api/rooms/room-1', expect.anything())
    expect(result.current.data).toMatchObject({ warning_message: 'expiring soon' })
  })

  it('stops polling once an unexpired qr_url arrives', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: {
          room_id: 'room-1',
          status: 'Running',
          qr_url: 'data:image/png;base64,abc',
          expires_at: new Date(Date.now() + 60_000).toISOString(),
          upstream_attendance_label: 'Class Attendance 1',
        },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderQuery(() => useRoomQuery('room-1', true), queryClient)

    // Flush the microtask chain that resolves the mocked fetch and setData.
    await act(async () => {
      for (let i = 0; i < 5; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
    })

    expect(result.current.data?.qr_url).toBe('data:image/png;base64,abc')
    expect(fetchMock).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(10_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps polling while Humantix verification is still pending', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: {
          room_id: 'room-1',
          status: 'Running',
          qr_url: 'data:image/png;base64,abc',
          expires_at: new Date(Date.now() + 60_000).toISOString(),
        },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    renderQuery(() => useRoomQuery('room-1', true), queryClient)

    await act(async () => {
      for (let i = 0; i < 5; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps polling when the cached QR is already expired', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: {
          room_id: 'room-1',
          status: 'Running',
          qr_url: 'data:image/png;base64,expired',
          expires_at: new Date(Date.now() - 1_000).toISOString(),
        },
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    renderQuery(() => useRoomQuery('room-1', true), queryClient)

    await act(async () => {
      for (let i = 0; i < 5; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('resumes polling while the room is still starting (no qr_url yet)', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({
        success: true,
        data: { room_id: 'room-1', status: 'starting' },
      }),
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ))
    const queryClient = makeQueryClient()
    const { result } = renderQuery(() => useRoomQuery('room-1', true), queryClient)

    await act(async () => {
      for (let i = 0; i < 5; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
    })

    expect(result.current.data?.qr_url).toBeUndefined()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Flat 1s poll keeps fetching until qr_url arrives.
    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})
