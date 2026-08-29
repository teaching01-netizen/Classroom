import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { courseIdSchema } from '@/features/courses'
import { sessionIdSchema, sessionKeys } from '@/features/sessions'
import { studentIdSchema, type SessionDetail } from './checkin.schemas'
import { useCheckinMutation } from './checkin.queries'

const courseId = courseIdSchema.parse('course-1')
const sessionId = sessionIdSchema.parse('session-1')
const studentId = studentIdSchema.parse('W123')

const uncheckedDetail: SessionDetail = {
  session_id: sessionId,
  session_number: 1,
  name: 'Class 1',
  date: '2026-08-29',
  checked_in_count: 0,
  total_students: 1,
  status: 'active',
  qr_active: false,
  students: [{
    student_id: studentId,
    name: 'Student One',
    nickname: '',
    school: '',
    avatar_url: '',
    checked_in: false,
    participation_points: 0,
  }],
}

const snapshot = {
  version: 7,
  generatedAt: '2026-08-29T10:00:00Z',
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderMutation(queryClient: QueryClient) {
  return renderHook(() => useCheckinMutation(courseId, sessionId), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
}

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ success: true, data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useCheckinMutation', () => {
  it('uses the verified PUT contract and replaces optimistic state with Humanix read-back', async () => {
    const checkedDetail: SessionDetail = {
      ...uncheckedDetail,
      checked_in_count: 1,
      students: [{ ...uncheckedDetail.students[0]!, checked_in: true }],
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({
        status: 'confirmed',
        checkedIn: true,
        snapshotVersion: 8,
        refreshPending: false,
      }))
      .mockResolvedValueOnce(response(checkedDetail))
    const queryClient = makeQueryClient()
    const queryKey = sessionKeys.detail(courseId, sessionId)
    queryClient.setQueryData(queryKey, { detail: uncheckedDetail, snapshot })
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({
        studentId,
        checkedIn: true,
        expectedSnapshotVersion: 7,
        idempotencyKey: 'checkin-key-1',
      })
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [url, init] = fetchMock.mock.calls[0] ?? []
    expect(String(url)).toBe('/api/teacher/courses/course-1/sessions/session-1/students/W123/checkin')
    expect(init?.method).toBe('PUT')
    expect(JSON.parse(String(init?.body))).toEqual({
      checkedIn: true,
      expectedSnapshotVersion: 7,
      idempotencyKey: 'checkin-key-1',
    })
    expect(queryClient.getQueryData<{ detail: SessionDetail }>(queryKey)?.detail)
      .toEqual(checkedDetail)
  })

  it('restores the previous row state when Humanix rejects the change', async () => {
    let rejectRequest: ((error: Error) => void) | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise((_resolve, reject) => {
      rejectRequest = reject
    }))
    const queryClient = makeQueryClient()
    const queryKey = sessionKeys.detail(courseId, sessionId)
    queryClient.setQueryData(queryKey, { detail: uncheckedDetail, snapshot })
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({
        studentId,
        checkedIn: true,
        expectedSnapshotVersion: 7,
        idempotencyKey: 'checkin-key-2',
      })
    })
    await waitFor(() => {
      expect(queryClient.getQueryData<{ detail: SessionDetail }>(queryKey)?.detail.students[0]?.checked_in)
        .toBe(true)
    })

    act(() => rejectRequest?.(new Error('Humanix rejected the change')))
    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(queryClient.getQueryData<{ detail: SessionDetail }>(queryKey)?.detail)
      .toEqual(uncheckedDetail)
  })

  it('keeps verifying a pending response until the authoritative roster changes', async () => {
    const checkedDetail: SessionDetail = {
      ...uncheckedDetail,
      checked_in_count: 1,
      students: [{ ...uncheckedDetail.students[0]!, checked_in: true }],
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({
        status: 'pending_verification',
        checkedIn: true,
        snapshotVersion: 7,
        refreshPending: true,
      }, 202))
      .mockResolvedValueOnce(response(uncheckedDetail))
      .mockResolvedValueOnce(response(checkedDetail))
    const queryClient = makeQueryClient()
    const queryKey = sessionKeys.detail(courseId, sessionId)
    queryClient.setQueryData(queryKey, { detail: uncheckedDetail, snapshot })
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({
        studentId,
        checkedIn: true,
        expectedSnapshotVersion: 7,
        idempotencyKey: 'checkin-key-pending',
      })
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true), { timeout: 2_000 })

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(result.current.data?.status).toBe('confirmed')
    expect(queryClient.getQueryData<{ detail: SessionDetail }>(queryKey)?.detail)
      .toEqual(checkedDetail)
  })

  it('restores authoritative state when verification times out', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response({
        status: 'pending_verification',
        checkedIn: true,
        snapshotVersion: 7,
        refreshPending: true,
      }, 202))
      .mockResolvedValueOnce(response(uncheckedDetail))
      .mockResolvedValueOnce(response(uncheckedDetail))
      .mockResolvedValueOnce(response(uncheckedDetail))
    const queryClient = makeQueryClient()
    const queryKey = sessionKeys.detail(courseId, sessionId)
    queryClient.setQueryData(queryKey, { detail: uncheckedDetail, snapshot })
    const { result } = renderMutation(queryClient)

    act(() => {
      result.current.mutate({
        studentId,
        checkedIn: true,
        expectedSnapshotVersion: 7,
        idempotencyKey: 'checkin-key-timeout',
      })
    })
    await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 3_000 })

    expect(fetchMock).toHaveBeenCalledTimes(4)
    expect(queryClient.getQueryData<{ detail: SessionDetail }>(queryKey)?.detail)
      .toEqual(uncheckedDetail)
  })
})
