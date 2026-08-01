import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { QrDialog } from './QrDialog'

const baseProps = {
  open: true,
  refreshing: false,
  onClose: vi.fn(),
  onRefresh: vi.fn(),
}

describe('QrDialog', () => {
  it('shows a spinner while the QR is pending', () => {
    render(<QrDialog {...baseProps} />)
    expect(screen.getByText('Generating a fresh QR code…')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('renders the QR image once available', () => {
    render(<QrDialog {...baseProps} qrUrl="data:image/png;base64,abc" />)
    expect(screen.getByRole('img')).toHaveAttribute('src', 'data:image/png;base64,abc')
  })

  it('renders without roster-derived counts', () => {
    render(<QrDialog {...baseProps} qrUrl="data:image/png;base64,abc" />)
    expect(screen.queryByText(/students checked in/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh QR code' })).toBeInTheDocument()
  })

  it('shows the count when the roster has loaded', () => {
    render(
      <QrDialog
        {...baseProps}
        qrUrl="data:image/png;base64,abc"
        checkedCount={3}
        totalCount={10}
      />,
    )
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('of 10 students checked in')).toBeInTheDocument()
  })
})
