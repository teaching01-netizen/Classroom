import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Pagination } from './Pagination'

describe('Pagination', () => {
  it('enforces page boundaries and announces the current page', () => {
    // Given
    const onPageChange = vi.fn()
    // When
    render(
      <Pagination
        page={1}
        pageSize={10}
        totalItems={21}
        onPageChange={onPageChange}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    // Then
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    expect(screen.getByText('Page 1 of 3')).toBeInTheDocument()
    expect(onPageChange).toHaveBeenCalledWith(2)
  })
})
