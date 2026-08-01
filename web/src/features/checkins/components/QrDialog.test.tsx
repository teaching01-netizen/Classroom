import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { QrDialog } from './QrDialog'

const baseProps = {
  open: true,
  refreshing: false,
  onClose: vi.fn(),
  onRefresh: vi.fn(),
}

describe('QrDialog', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

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

  it('shows a countdown from the QR expiry', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-01T10:00:00Z'))
    render(
      <QrDialog
        {...baseProps}
        qrUrl="data:image/png;base64,abc"
        expiresAt="2026-08-01T10:02:00Z"
      />,
    )
    expect(screen.getByText('Expires in: 120s')).toBeInTheDocument()
  })

  it('marks the countdown urgent at or under 10 seconds', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-01T10:00:00Z'))
    render(
      <QrDialog
        {...baseProps}
        qrUrl="data:image/png;base64,abc"
        expiresAt="2026-08-01T10:00:05Z"
      />,
    )
    expect(screen.getByText('Expires in: 5s')).toHaveClass('qr-dialog__expiry--urgent')
  })

  it('warns when a QR is shown without an expiry', () => {
    render(<QrDialog {...baseProps} qrUrl="data:image/png;base64,abc" />)
    expect(screen.getByRole('alert')).toHaveTextContent('may no longer be valid')
  })

  it('warns when the QR has expired', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-01T10:00:00Z'))
    render(
      <QrDialog
        {...baseProps}
        qrUrl="data:image/png;base64,abc"
        expiresAt="2026-08-01T09:59:00Z"
      />,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('may no longer be valid')
  })

  it('suppresses the warning while a refresh is in flight', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-01T10:00:00Z'))
    render(
      <QrDialog
        {...baseProps}
        refreshing
        qrUrl="data:image/png;base64,abc"
        expiresAt="2026-08-01T09:59:00Z"
      />,
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the backend error instead of the spinner when the QR cannot be generated', () => {
    render(
      <QrDialog {...baseProps} errorMessage="Session recovery failed after 10 attempts" />,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('Session recovery failed after 10 attempts')
    expect(screen.queryByText('Generating a fresh QR code…')).not.toBeInTheDocument()
  })

  it('shows the backend warning instead of the spinner while recovery is in progress', () => {
    render(
      <QrDialog {...baseProps} warningMessage="Session expired, retrying..." />,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('Session expired, retrying...')
    expect(screen.queryByText('Generating a fresh QR code…')).not.toBeInTheDocument()
  })
})
