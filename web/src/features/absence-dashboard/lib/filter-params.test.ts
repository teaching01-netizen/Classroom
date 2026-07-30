import { describe, expect, it } from 'vitest'
import { courseIdSchema } from '@/features/courses'
import { filtersFromSearchParams, filtersToSearchParams } from './filter-params'

describe('dashboard filter URL serialization', () => {
  it('round-trips shareable filters through search parameters', () => {
    // Given
    const filters = {
      courseIds: [courseIdSchema.parse('CS101')],
      dateRange: { from: '2026-01-01', to: '2026-03-31' },
      threshold: 3,
      sortBy: 'rate-asc' as const,
      wCodes: ['u123'],
    }
    // When
    const parsed = filtersFromSearchParams(filtersToSearchParams(filters))
    // Then
    expect(parsed).toEqual(filters)
  })

  it('uses safe defaults for invalid URL values', () => {
    // Given
    const params = new URLSearchParams('threshold=oops&sort=nope')
    // When
    const filters = filtersFromSearchParams(params)
    // Then
    expect(filters).toEqual({
      courseIds: [],
      dateRange: null,
      threshold: 0,
      sortBy: 'risk',
      wCodes: [],
    })
  })
})
