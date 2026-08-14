import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@/shared/ui/Toast'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { Component as AbsenceDashboardRoute } from './AbsenceDashboardRoute'

const downloadAbsenceDashboard = vi.fn()
let reportData: AbsenceDashboard | undefined = undefined

vi.mock('../lib/csv', () => ({
  downloadAbsenceDashboard: (report: unknown) => downloadAbsenceDashboard(report),
}))

vi.mock('../api/absence.queries', () => ({
  useAbsenceDashboardQuery: () => ({
    data: reportData,
    isPending: false,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  }),
  useCoursesQuery: () => ({ data: { courses: [] }, isLoading: false, error: null }),
  useDashboardViewsQuery: () => ({ data: [] }),
  useSaveDashboardViewMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteDashboardViewMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useTouchDashboardViewMutation: () => ({ mutate: vi.fn(), isPending: false }),
}))

const emptyReport: AbsenceDashboard = {
  generatedAt: '2026-06-20T10:00:00Z',
  totalStudents: 0,
  totalCourses: 0,
  avgAttendanceRate: 0,
  atRiskCount: 0,
  topAtRisk: [],
  students: [],
  sessions: [],
}

function renderRoute() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <MemoryRouter initialEntries={['/absence-dashboard?courses=c1&load=1']}>
          <Routes>
            <Route path="/absence-dashboard" element={<AbsenceDashboardRoute />} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </QueryClientProvider>,
  )
}

describe('AbsenceDashboardRoute export', () => {
  beforeEach(() => {
    reportData = undefined
    downloadAbsenceDashboard.mockReset()
  })

  it('downloads the loaded report as CSV', () => {
    // Given
    reportData = emptyReport
    renderRoute()

    // When
    fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }))

    // Then
    expect(downloadAbsenceDashboard).toHaveBeenCalledWith(emptyReport)
  })

  it('keeps the export button disabled until a report is loaded', () => {
    // Given
    renderRoute()

    // Then
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled()
  })
})
