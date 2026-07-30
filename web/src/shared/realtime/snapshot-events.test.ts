import { beforeEach, describe, expect, it } from 'vitest'
import {
  acceptSnapshotCommitted,
  resetSnapshotVersionsForTests,
  snapshotMetadataSchema,
  snapshotVersionKey,
} from './snapshot-events'

describe('snapshot version matching', () => {
  beforeEach(() => resetSnapshotVersionsForTests())

  it('accepts only increasing versions for the same resource', () => {
    // Given
    const first = snapshotMetadataSchema.parse({
      kind: 'session_detail',
      parent_key: 'course-1',
      resource_key: 'session-1',
      version: 2,
    })
    // When
    const outcomes = [
      acceptSnapshotCommitted(first),
      acceptSnapshotCommitted(first),
      acceptSnapshotCommitted({ ...first, version: 3 }),
    ]
    // Then
    expect(snapshotVersionKey(first)).toBe('session_detail\u0000course-1\u0000session-1')
    expect(outcomes).toEqual([true, false, true])
  })
})
