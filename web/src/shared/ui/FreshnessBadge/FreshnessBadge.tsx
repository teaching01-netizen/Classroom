import { useEffect, useState } from 'react'
import { Badge, type BadgeTone } from '@/shared/ui/Badge'
import './FreshnessBadge.css'

export type FreshnessBadgeProps = {
  // RFC3339 timestamp of when the underlying snapshot data was last
  // validated. Absent when the server runs without snapshots (live mode).
  readonly generatedAt?: string | undefined
  readonly stale?: boolean | undefined
  readonly quality?: string | undefined
  // Live marks a realtime WebSocket connection: data updates as it happens.
  readonly live?: boolean | undefined
}

const FRESH_MS = 5 * 60 * 1000
const WARNING_MS = 30 * 60 * 1000
const TICK_MS = 30 * 1000

function formatRelative(ageMs: number): string {
  const minutes = Math.floor(ageMs / 60_000)
  if (minutes < 1) {
    return 'just now'
  }
  if (minutes < 60) {
    return `${minutes} min ago`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `${hours} h ago`
  }
  return `${Math.floor(hours / 24)} d ago`
}

function formatClock(date: Date): string {
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${hours}:${minutes}`
}

function formatExactTime(iso: string): string {
  const date = new Date(iso)
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
  return `${date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })} (${timeZone})`
}

export function FreshnessBadge({
  generatedAt,
  stale,
  quality,
  live = false,
}: FreshnessBadgeProps) {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), TICK_MS)
    return () => window.clearInterval(id)
  }, [])

  if (live) {
    // A live connection means the data is current as of this moment; fall
    // back to the snapshot time when one exists.
    const verifiedAt = generatedAt ?? new Date(now).toISOString()
    return (
      <Badge tone="success" title={`Last verified ${formatExactTime(verifiedAt)}`}>
        <span aria-hidden="true" className="freshness-dot" />
        <span>Live — updates in real time</span>
      </Badge>
    )
  }

  if (generatedAt === undefined || generatedAt === '') {
    return null
  }
  const generated = new Date(generatedAt)
  if (Number.isNaN(generated.getTime())) {
    return null
  }

  const ageMs = Math.max(0, now - generated.getTime())
  const relative = formatRelative(ageMs)
  const degraded = stale === true || (quality !== undefined && quality !== 'verified_fresh')

  if (degraded) {
    return (
      <Badge tone="warning" title={`Data last verified ${formatExactTime(generatedAt)} — may be outdated`}>
        <span aria-hidden="true" className="freshness-dot freshness-dot--static" />
        <span>Data last verified {relative} — may be outdated</span>
      </Badge>
    )
  }

  const tone: BadgeTone =
    ageMs <= FRESH_MS ? 'success' : ageMs <= WARNING_MS ? 'warning' : 'danger'
  return (
    <Badge tone={tone} title={`Data as of ${formatExactTime(generatedAt)}`}>
      <span aria-hidden="true" className="freshness-dot freshness-dot--static" />
      <span>
        Data as of <time className="freshness-time" dateTime={generatedAt}>{formatClock(generated)}</time>
        {' · '}
        {relative}
      </span>
    </Badge>
  )
}
