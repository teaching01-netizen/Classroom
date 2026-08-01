import { describe, expect, it } from 'vitest'
import { applyCheckinDelta, isSessionDetailLike } from './checkin-update'

const detail = {
  session_id: 'session-1',
  name: 'Session 1',
  students: [
    { student_id: 'a', name: 'Alice', checked_in: false },
    { student_id: 'b', name: 'Bob', checked_in: true },
  ],
  checked_in_count: 1,
}

describe('applyCheckinDelta', () => {
  it('flips the matching student and refreshes the count', () => {
    const next = applyCheckinDelta(detail, 'a', true)
    expect(next.students[0]?.checked_in).toBe(true)
    expect(next.students[1]?.checked_in).toBe(true)
    expect(next.checked_in_count).toBe(2)
    // Unrelated fields are preserved.
    expect(next.name).toBe('Session 1')
    // The original cache entry is not mutated.
    expect(detail.students[0]?.checked_in).toBe(false)
    expect(detail.checked_in_count).toBe(1)
  })

  it('leaves unmatched students untouched', () => {
    const next = applyCheckinDelta(detail, 'unknown', true)
    expect(next.students).toEqual(detail.students)
    expect(next.checked_in_count).toBe(1)
  })

  it('can un-check a student', () => {
    const next = applyCheckinDelta(detail, 'b', false)
    expect(next.students[1]?.checked_in).toBe(false)
    expect(next.checked_in_count).toBe(0)
  })
})

describe('isSessionDetailLike', () => {
  it('accepts session-detail-shaped cache entries', () => {
    expect(isSessionDetailLike(detail)).toBe(true)
    expect(isSessionDetailLike({ students: [] })).toBe(true)
  })

  it('rejects course-detail-shaped entries and non-objects', () => {
    expect(isSessionDetailLike({ sessions: [] })).toBe(false)
    expect(isSessionDetailLike(null)).toBe(false)
    expect(isSessionDetailLike(undefined)).toBe(false)
    expect(isSessionDetailLike('students')).toBe(false)
  })
})
