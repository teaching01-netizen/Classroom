import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CourseId } from '@/features/courses'
import type { SessionId } from '@/features/sessions'

const mocks = vi.hoisted(() => {
  const roomQuery = {
    data: undefined as
      | { qr_url?: string | null; expires_at?: string | null; status?: string }
      | undefined,
    refetch: vi.fn(),
    isFetching: false,
  }
  return {
    roomQuery,
    roomQueryArgs: vi.fn(),
    startRoom: vi.fn(),
    unstableRoomQueryResult: false,
  }
})

vi.mock('@/features/rooms', () => ({
  useStartRoomMutation: () => ({ mutateAsync: mocks.startRoom, isPending: false }),
  useRoomQuery: (roomId: string | undefined, enabled: boolean) => {
    mocks.roomQueryArgs(roomId, enabled)
    return mocks.unstableRoomQueryResult ? { ...mocks.roomQuery } : mocks.roomQuery
  },
}))

vi.mock('@/shared/lib/errors', () => ({
  getErrorMessage: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}))

vi.mock('@/shared/ui/Toast', () => ({
  useToast: () => ({ announce: vi.fn() }),
}))

import { useSessionQr } from './useSessionQr'

const courseId = 'course-1' as CourseId
const sessionId = 'session-1' as SessionId

function qrData(expiresAt: string) {
  return { qr_url: 'data:image/png;base64,abc', expires_at: expiresAt }
}

describe('useSessionQr', () => {
  beforeEach(() => {
    mocks.startRoom.mockReset()
    mocks.roomQueryArgs.mockReset()
    mocks.roomQuery.refetch.mockReset()
    mocks.roomQuery.data = undefined
    mocks.unstableRoomQueryResult = false
    mocks.startRoom.mockResolvedValue({ roomId: 'room-1' })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts the QR flow on mount and opens once a room id is known', async () => {
    const { result } = renderHook(() => useSessionQr(courseId, sessionId))

    await waitFor(() => expect(result.current.open).toBe(true))
    expect(mocks.startRoom).toHaveBeenCalledTimes(1)
  })

  it('refreshes immediately when the displayed QR is already expired', async () => {
    mocks.roomQuery.data = qrData(new Date(Date.now() - 1_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))

    await waitFor(() => expect(result.current.open).toBe(true))
    expect(mocks.startRoom).toHaveBeenCalledTimes(2)
  })

  it('coalesces expired auto-refresh while the room version has not advanced', async () => {
    mocks.roomQuery.data = qrData(new Date(Date.now() - 1_000).toISOString())
    mocks.unstableRoomQueryResult = true

    const { result, rerender } = renderHook(() => useSessionQr(courseId, sessionId))

    await waitFor(() => expect(result.current.open).toBe(true))
    expect(mocks.startRoom).toHaveBeenCalledTimes(2)

    rerender()
    rerender()
    rerender()

    expect(mocks.startRoom).toHaveBeenCalledTimes(2)
  })

  it('schedules a single refresh shortly before expiry', async () => {
    vi.useFakeTimers()
    mocks.roomQuery.data = qrData(new Date(Date.now() + 60_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))
    await act(async () => vi.advanceTimersByTimeAsync(0))
    expect(result.current.open).toBe(true)
    expect(mocks.startRoom).toHaveBeenCalledTimes(1)

    // 49s in: refresh is scheduled for 50s (10s ahead of a 60s expiry).
    act(() => vi.advanceTimersByTime(49_000))
    expect(mocks.startRoom).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(1_000))
    expect(mocks.startRoom).toHaveBeenCalledTimes(2)
  })

  it('does not refresh while the dialog is closed', () => {
    vi.useFakeTimers()
    mocks.startRoom.mockImplementation(() => new Promise(() => {})) // room never resolves → dialog stays closed
    mocks.roomQuery.data = qrData(new Date(Date.now() - 1_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))
    expect(result.current.open).toBe(false)

    act(() => vi.advanceTimersByTime(120_000))
    expect(mocks.startRoom).toHaveBeenCalledTimes(1)
  })

  it('re-arms the refresh timer when the expiry changes', async () => {
    vi.useFakeTimers()
    mocks.roomQuery.data = qrData(new Date(Date.now() + 60_000).toISOString())

    const { result, rerender } = renderHook(() => useSessionQr(courseId, sessionId))
    await act(async () => vi.advanceTimersByTimeAsync(0))
    expect(result.current.open).toBe(true)
    expect(mocks.startRoom).toHaveBeenCalledTimes(1)

    // The QR is refreshed to a shorter-lived one while the dialog is open.
    mocks.roomQuery.data = qrData(new Date(Date.now() + 30_000).toISOString())
    rerender()

    act(() => vi.advanceTimersByTime(20_000))
    expect(mocks.startRoom).toHaveBeenCalledTimes(2)
  })

  it('starts a new QR room when the route changes to another session', async () => {
    const sessionTwo = 'session-2' as SessionId
    const { rerender } = renderHook(
      ({ session }) => useSessionQr(courseId, session),
      { initialProps: { session: sessionId } },
    )

    expect(mocks.startRoom).toHaveBeenCalledTimes(1)
    expect(mocks.startRoom).toHaveBeenLastCalledWith({ sessionId, courseId })

    rerender({ session: sessionTwo })

    await waitFor(() => expect(mocks.startRoom).toHaveBeenCalledTimes(2))
    expect(mocks.startRoom).toHaveBeenLastCalledWith({ sessionId: sessionTwo, courseId })
  })

  it('ignores a late room-start result from the previous session', async () => {
    const sessionTwo = 'session-2' as SessionId
    const completions = new Map<string, (result: { roomId: string }) => void>()
    mocks.startRoom.mockImplementation((variables: { sessionId: string }) =>
      new Promise((resolve) => completions.set(variables.sessionId, resolve)),
    )

    const { rerender } = renderHook(
      ({ session }) => useSessionQr(courseId, session),
      { initialProps: { session: sessionId } },
    )
    rerender({ session: sessionTwo })

    await waitFor(() => expect(mocks.startRoom).toHaveBeenCalledTimes(2))
    act(() => completions.get(sessionTwo)?.({ roomId: 'room-2' }))
    await waitFor(() => expect(mocks.roomQueryArgs).toHaveBeenLastCalledWith('room-2', true))

    act(() => completions.get(sessionId)?.({ roomId: 'room-1' }))
    expect(mocks.roomQueryArgs).toHaveBeenLastCalledWith('room-2', true)
  })

  it('runs the initial start mutation exactly once under StrictMode', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <StrictMode>{children}</StrictMode>
    )

    renderHook(() => useSessionQr(courseId, sessionId), { wrapper })

    expect(mocks.startRoom).toHaveBeenCalledTimes(1)
  })
})
