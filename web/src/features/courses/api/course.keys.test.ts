import { describe, expect, it } from 'vitest'
import { courseIdSchema } from './course.schemas'
import { courseKeys } from './course.keys'

describe('course query keys', () => {
  it('creates stable list and nested detail keys', () => {
    // Given
    const courseId = courseIdSchema.parse('CS101')
    // When
    const listKey = courseKeys.list()
    const detailKey = courseKeys.detail(courseId)
    // Then
    expect(listKey).toEqual(['courses', 'list', { search: '', status: 'all' }])
    expect(detailKey).toEqual(['courses', 'detail', 'CS101'])
  })
})
