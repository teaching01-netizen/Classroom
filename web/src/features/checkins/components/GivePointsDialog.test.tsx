import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { GivePointsDialog } from './GivePointsDialog'

function renderDialog(overrides: { onSubmit?: (points: number) => void } = {}) {
  const onSubmit = overrides.onSubmit ?? vi.fn()
  render(
    <GivePointsDialog
      open
      studentName="Alice Chen"
      onClose={() => {}}
      onSubmit={onSubmit}
      pending={false}
    />,
  )
  return onSubmit
}

describe('GivePointsDialog', () => {
  it('names the student it will award', () => {
    renderDialog()
    expect(screen.getByText(/Alice Chen/)).toBeInTheDocument()
  })

  it('submits the chosen point count', () => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByRole('spinbutton', { name: /points/i }), { target: { value: '2' } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).toHaveBeenCalledWith(2)
  })

  it.each([['0'], ['11'], ['2.5'], ['']])('refuses %s without calling onSubmit', (value) => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByRole('spinbutton', { name: /points/i }), { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByText(/between 1 and 10/i)).toBeTruthy()
  })

  it.each([['1'], ['10']])('accepts the boundary value %s', (value) => {
    const onSubmit = renderDialog()

    fireEvent.change(screen.getByRole('spinbutton', { name: /points/i }), { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: /award points/i }))

    expect(onSubmit).toHaveBeenCalledWith(Number(value))
    cleanup()
  })
})
