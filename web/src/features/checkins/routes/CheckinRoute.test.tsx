import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Component as CheckinRoute } from './CheckinRoute'

const useSessionQrMock = vi.fn()

vi.mock('@/features/sessions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/sessions')>()),
  useCourseSessionsQuery: () => ({ data: undefined }),
}))

vi.mock('../api/checkin.queries', () => ({
  useCheckinsQuery: () => ({
    data: undefined,
    isPending: true,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  }),
  useSessionSnapshotQuery: () => ({
    data: undefined,
  }),
  useToggleCheckinMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}))

vi.mock('../hooks/useSessionQr', () => ({
  useSessionQr: (courseId: string, sessionId: string) => {
    useSessionQrMock(courseId, sessionId)
    return {
      qrUrl: undefined,
      open: false,
      refreshing: false,
      openQr: vi.fn(),
      closeQr: vi.fn(),
      refresh: vi.fn(),
    }
  },
}))

vi.mock('@/shared/ui/Toast', () => ({
  useToast: () => ({ announce: vi.fn() }),
}))

function renderRoute() {
  return render(
    <MemoryRouter initialEntries={['/courses/course-1/sessions/session-1']}>
      <Routes>
        <Route path="/courses/:courseId/sessions/:sessionId" element={<CheckinRoute />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('CheckinRoute', () => {
  beforeEach(() => {
    useSessionQrMock.mockClear()
  })

  it('starts the QR flow independently of the roster query', () => {
    renderRoute()
    // The roster query is pending, yet the QR entry point renders and the
    // session-QR hook was invoked with the route params.
    expect(useSessionQrMock).toHaveBeenCalledWith('course-1', 'session-1')
    expect(screen.getByRole('button', { name: 'View QR code' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeInTheDocument()
  })
})
