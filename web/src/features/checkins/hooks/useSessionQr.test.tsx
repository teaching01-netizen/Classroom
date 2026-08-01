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
    startRoomMutate: vi.fn(),
  }
})

vi.mock('@/features/rooms', () => ({
  useStartRoomMutation: () => ({ mutate: mocks.startRoomMutate, isPending: false }),
  useRoomQuery: () => mocks.roomQuery,
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
    mocks.startRoomMutate.mockReset()
    mocks.roomQuery.refetch.mockReset()
    mocks.roomQuery.data = undefined
    // Default: the start mutation resolves immediately with a room id.
    mocks.startRoomMutate.mockImplementation((_vars: unknown, opts?: { onSuccess?: (r: { roomId: string }) => void }) => {
      opts?.onSuccess?.({ roomId: 'room-1' })
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts the QR flow on mount and opens once a room id is known', async () => {
    const { result } = renderHook(() => useSessionQr(courseId, sessionId))

    await waitFor(() => expect(result.current.open).toBe(true))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)
  })

  it('refreshes immediately when the displayed QR is already expired', async () => {
    mocks.roomQuery.data = qrData(new Date(Date.now() - 1_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))

    await waitFor(() => expect(result.current.open).toBe(true))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(2)
  })

  it('schedules a single refresh shortly before expiry', () => {
    vi.useFakeTimers()
    mocks.roomQuery.data = qrData(new Date(Date.now() + 60_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))
    expect(result.current.open).toBe(true)
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)

    // 49s in: refresh is scheduled for 50s (10s ahead of a 60s expiry).
    act(() => vi.advanceTimersByTime(49_000))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(1_000))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(2)
  })

  it('does not refresh while the dialog is closed', () => {
    vi.useFakeTimers()
    mocks.startRoomMutate.mockImplementation(() => {}) // room never resolves → dialog stays closed
    mocks.roomQuery.data = qrData(new Date(Date.now() - 1_000).toISOString())

    const { result } = renderHook(() => useSessionQr(courseId, sessionId))
    expect(result.current.open).toBe(false)

    act(() => vi.advanceTimersByTime(120_000))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)
  })

  it('re-arms the refresh timer when the expiry changes', () => {
    vi.useFakeTimers()
    mocks.roomQuery.data = qrData(new Date(Date.now() + 60_000).toISOString())

    const { result, rerender } = renderHook(() => useSessionQr(courseId, sessionId))
    expect(result.current.open).toBe(true)
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)

    // The QR is refreshed to a shorter-lived one while the dialog is open.
    mocks.roomQuery.data = qrData(new Date(Date.now() + 30_000).toISOString())
    rerender()

    act(() => vi.advanceTimersByTime(20_000))
    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(2)
  })

  it('runs the initial start mutation exactly once under StrictMode', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <StrictMode>{children}</StrictMode>
    )

    renderHook(() => useSessionQr(courseId, sessionId), { wrapper })

    expect(mocks.startRoomMutate).toHaveBeenCalledTimes(1)
  })
})
