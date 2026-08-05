import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Component as AttendanceRoute } from './AttendanceRoute'

const refetch = vi.fn()
const downloadAttendanceReport = vi.fn()
let isFetching = false
let resolveDownload: (() => void) | null = null

vi.mock('../api/attendance.queries', () => ({
  useAttendanceQuery: () => ({
    data: {
      courseId: 'course-1',
      courseName: 'Course 1',
      sessions: [],
      students: [],
      errors: [],
      truncated: false,
      stale: true,
      threshold: 1,
      computedAt: '2026-08-02T12:00:00Z',
      durationMs: 20,
    },
    isPending: false,
    isFetching,
    error: null,
    refetch,
  }),
  useAttendanceSnapshotQuery: () => ({
    data: undefined,
  }),
}))

vi.mock('../api/attendance.exports', () => ({
  downloadAttendanceReport: (options: unknown) => downloadAttendanceReport(options),
}))

function renderRoute() {
  return render(
    <MemoryRouter initialEntries={['/courses/course-1/attendance']}>
      <Routes>
        <Route path="/courses/:courseId/attendance" element={<AttendanceRoute />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('AttendanceRoute exports', () => {
  beforeEach(() => {
    isFetching = false
    resolveDownload = null
    refetch.mockReset().mockResolvedValue(undefined)
    downloadAttendanceReport.mockReset().mockResolvedValue({ filename: 'attendance.csv' })
  })

  it('downloads CSV and refetches the report after success', async () => {
    // Given
    renderRoute()

    // When
    fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }))

    // Then
    await waitFor(() => {
      expect(downloadAttendanceReport).toHaveBeenCalledWith({
        courseId: 'course-1',
        format: 'csv',
        threshold: 1,
      })
      expect(refetch).toHaveBeenCalledOnce()
    })
  })

  it('shows export progress, disables exports, and refetches after the download resolves', async () => {
    // Given
    downloadAttendanceReport.mockImplementation(() => new Promise<{ filename: string }>((resolve) => {
      resolveDownload = () => resolve({ filename: 'attendance.csv' })
    }))
    renderRoute()

    // When
    fireEvent.click(screen.getByRole('button', { name: 'Export CSV' }))

    // Then
    expect(await screen.findByText('Refreshing latest data…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Export Excel' })).toBeDisabled()
    expect(refetch).not.toHaveBeenCalled()

    if (resolveDownload === null) {
      throw new Error('Expected the export download to be pending')
    }
    resolveDownload()

    await waitFor(() => {
      expect(refetch).toHaveBeenCalledOnce()
    })
  })

  it('shows stale and refreshing warnings and disables exports while fetching', () => {
    // Given
    isFetching = true

    // When
    renderRoute()

    // Then
    expect(screen.getByText('Refreshing latest data…')).toBeInTheDocument()
    expect(screen.getByText(/stored attendance report may be out of date/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Export Excel' })).toBeDisabled()
  })

  it('triggers a single export request when the CSV button is double-clicked', async () => {
    // Given
    downloadAttendanceReport.mockImplementation(() => new Promise<{ filename: string }>((resolve) => {
      resolveDownload = () => resolve({ filename: 'attendance.csv' })
    }))
    renderRoute()

    // When
    const csvButton = screen.getByRole('button', { name: 'Export CSV' })
    fireEvent.click(csvButton)
    fireEvent.click(csvButton)

    // Then
    expect(downloadAttendanceReport).toHaveBeenCalledTimes(1)
    expect(refetch).not.toHaveBeenCalled()
    if (resolveDownload === null) {
      throw new Error('Expected the export download to be pending')
    }
    resolveDownload()
    await waitFor(() => {
      expect(refetch).toHaveBeenCalledOnce()
    })
    expect(downloadAttendanceReport).toHaveBeenCalledTimes(1)
  })

  it('returns focus to the initiating export control after the download finishes', async () => {
    // Given
    downloadAttendanceReport.mockImplementation(() => new Promise<{ filename: string }>((resolve) => {
      resolveDownload = () => resolve({ filename: 'attendance.xlsx' })
    }))
    renderRoute()

    // When
    const excelButton = screen.getByRole('button', { name: 'Export Excel' })
    excelButton.focus()
    fireEvent.click(excelButton)
    if (resolveDownload === null) {
      throw new Error('Expected the export download to be pending')
    }
    resolveDownload()

    // Then
    await waitFor(() => {
      expect(document.activeElement).toBe(excelButton)
    })
  })
})
