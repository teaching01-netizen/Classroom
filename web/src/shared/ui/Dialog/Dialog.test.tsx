import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Dialog } from './Dialog'

describe('Dialog', () => {
  it('opens as a named modal, moves focus inside, and closes from its control', () => {
    // Given
    const onClose = vi.fn()
    // When
    render(
      <Dialog open title="Save view" onClose={onClose}>
        <button type="button">Confirm save</button>
      </Dialog>,
    )
    const dialog = screen.getByRole('dialog', { name: 'Save view' })
    fireEvent.click(screen.getByRole('button', { name: 'Close dialog' }))
    // Then
    expect(dialog).toHaveAttribute('open')
    expect(dialog.contains(document.activeElement)).toBe(true)
    expect(onClose).toHaveBeenCalledOnce()
  })
})
