import { describe, expect, it } from 'vitest'
import { getAffectedQueryKeys } from './invalidation-map'
import { snapshotMetadataSchema } from './snapshot-events'

describe('realtime invalidation mapping', () => {
  it('targets session, course, attendance, and dashboard queries', () => {
    // Given
    const metadata = snapshotMetadataSchema.parse({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 1,
    })
    // When
    const keys = getAffectedQueryKeys(metadata)
    // Then
    expect(keys).toEqual([
      ['courses', 'detail', 'course-1'],
      ['sessions', 'course-1'],
      ['sessions', 'course-1', 'session-1'],
      ['attendance', 'course-1'],
      ['absence-dashboard'],
    ])
  })
})
