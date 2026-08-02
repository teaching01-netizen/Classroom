import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { courseIdSchema } from '@/features/courses'
import { ApiError } from '@/shared/api/api-error'
import { downloadAttendanceReport } from './attendance.exports'

describe('attendance report export', () => {
  const courseId = courseIdSchema.parse('course/1')
  const createObjectURL = vi.fn(() => 'blob:attendance-report')
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('URL', {
      ...URL,
      createObjectURL,
      revokeObjectURL,
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    createObjectURL.mockClear()
    revokeObjectURL.mockClear()
  })

  it('posts JSON and downloads the blob using the server filename', async () => {
    // Given
    const blob = new Blob(['csv-data'], { type: 'text/csv' })
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(blob, {
      status: 200,
      headers: {
        'Content-Disposition': `attachment; filename*=UTF-8''attendance-%E0%B8%A7%E0%B8%B4%E0%B8%8A%E0%B8%B2.csv`,
      },
    }))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)

    // When
    const result = await downloadAttendanceReport({ courseId, format: 'csv', threshold: 2 })

    // Then
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/teacher/courses/course%2F1/attendance-report/export',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ format: 'csv', threshold: 2 }),
        cache: 'no-store',
        headers: expect.any(Headers),
        signal: expect.any(AbortSignal),
      }),
    )
    expect(result.filename).toBe('attendance-วิชา.csv')
    expect(createObjectURL).toHaveBeenCalledWith(blob)
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:attendance-report')
    expect(document.querySelector('a[download]')).toBeNull()
  })

  it('parses a JSON API error instead of treating it as a blob', async () => {
    // Given
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(
      JSON.stringify({ success: false, error: 'Latest attendance data could not be validated. Please try again.' }),
      { status: 503, headers: { 'Content-Type': 'application/json' } },
    ))

    // When
    const request = downloadAttendanceReport({ courseId, format: 'xlsx', threshold: 0 })

    // Then
    await expect(request).rejects.toMatchObject({
      name: 'ApiError',
      kind: 'http',
      status: 503,
      message: 'Latest attendance data could not be validated. Please try again.',
    } satisfies Partial<ApiError>)
    expect(createObjectURL).not.toHaveBeenCalled()
    expect(revokeObjectURL).not.toHaveBeenCalled()
  })

  it('keeps abort cleanup safe before a blob URL exists', async () => {
    // Given
    const controller = new AbortController()
    controller.abort()
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new DOMException('Aborted', 'AbortError'))

    // When
    const request = downloadAttendanceReport({
      courseId,
      format: 'csv',
      threshold: 0,
      signal: controller.signal,
    })

    // Then
    await expect(request).rejects.toMatchObject({ kind: 'cancelled' })
    expect(createObjectURL).not.toHaveBeenCalled()
    expect(revokeObjectURL).not.toHaveBeenCalled()
    expect(document.querySelector('a[download]')).toBeNull()
  })
})
