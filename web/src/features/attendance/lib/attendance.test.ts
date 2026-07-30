import { describe, expect, it } from 'vitest'
import { attendancePercent, isAtRisk } from './attendance'

describe('attendance calculations', () => {
  it('rounds and clamps an attendance rate', () => {
    // Given
    const rates = [0.834, -1, 2] as const
    // When
    const percentages = rates.map(attendancePercent)
    // Then
    expect(percentages).toEqual([83, 0, 100])
  })

  it('classifies a student below the configured threshold as at risk', () => {
    // Given
    const threshold = 70
    // When
    const classifications = [isAtRisk(0.69, threshold), isAtRisk(0.7, threshold)]
    // Then
    expect(classifications).toEqual([true, false])
  })
})
