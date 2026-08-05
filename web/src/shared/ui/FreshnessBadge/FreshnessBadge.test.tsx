import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FreshnessBadge } from './FreshnessBadge'

// Local-time anchor so assertions on the HH:MM clock and relative labels are
// independent of the test machine's timezone.
const NOW = new Date(2026, 7, 5, 10, 0, 0)

function at(minutesAgo: number): string {
  return new Date(NOW.getTime() - minutesAgo * 60_000).toISOString()
}

function badgeTone(container: HTMLElement): string | null | undefined {
  return container.querySelector('.ui-badge')?.getAttribute('data-tone')
}

describe('FreshnessBadge', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders a fresh badge with the success tone and relative time', () => {
    const { container } = render(<FreshnessBadge generatedAt={at(2)} />)
    expect(badgeTone(container)).toBe('success')
    expect(screen.getByText(/Data as of/)).toHaveTextContent('2 min ago')
  })

  it('shows the local clock inside a native time element', () => {
    render(<FreshnessBadge generatedAt={at(2)} />)
    const time = screen.getByText('09:58')
    expect(time.tagName).toBe('TIME')
    expect(time).toHaveAttribute('dateTime', at(2))
  })

  it('warns when data is between 5 and 30 minutes old', () => {
    const { container } = render(<FreshnessBadge generatedAt={at(10)} />)
    expect(badgeTone(container)).toBe('warning')
  })

  it('flags data older than 30 minutes as danger', () => {
    const { container } = render(<FreshnessBadge generatedAt={at(45)} />)
    expect(badgeTone(container)).toBe('danger')
    expect(screen.getByText(/Data as of/)).toHaveTextContent('45 min ago')
  })

  it('marks stale data as potentially outdated', () => {
    const { container } = render(<FreshnessBadge generatedAt={at(2)} stale />)
    expect(badgeTone(container)).toBe('warning')
    expect(screen.getByText(/may be outdated/)).toHaveTextContent('Data last verified 2 min ago')
  })

  it('marks non-fresh quality as potentially outdated', () => {
    const { container } = render(<FreshnessBadge generatedAt={at(2)} quality="verified_stale" />)
    expect(badgeTone(container)).toBe('warning')
    expect(screen.getByText(/may be outdated/)).toBeInTheDocument()
  })

  it('shows the live badge while connected', () => {
    const { container } = render(<FreshnessBadge live />)
    expect(badgeTone(container)).toBe('success')
    expect(screen.getByText('Live — updates in real time')).toBeInTheDocument()
  })

  it('renders nothing when no snapshot time exists and the page is not live', () => {
    const { container } = render(<FreshnessBadge />)
    expect(container.firstChild).toBeNull()
  })

  it('says "just now" for data under a minute old', () => {
    render(<FreshnessBadge generatedAt={new Date(NOW.getTime() - 30_000).toISOString()} />)
    expect(screen.getByText(/Data as of/)).toHaveTextContent('just now')
  })

  it('uses day labels for data older than a day', () => {
    render(<FreshnessBadge generatedAt={at(25 * 60)} />)
    expect(screen.getByText(/Data as of/)).toHaveTextContent('1 d ago')
  })
})
