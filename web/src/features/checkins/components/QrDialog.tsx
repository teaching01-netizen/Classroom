import { useEffect, useState } from 'react'
import { Dialog } from '@/shared/ui/Dialog'
import { Button } from '@/shared/ui/Button'
import { Spinner } from '@/shared/ui/Spinner'

type QrDialogProps = {
  readonly open: boolean
  readonly qrUrl?: string | undefined
  readonly expiresAt?: string | undefined
  // Backend room state surfaced so a failed QR fetch is not an eternal
  // "Generating…" spinner: error/warning messages explain why no QR exists.
  readonly errorMessage?: string | undefined
  readonly warningMessage?: string | undefined
  // Roster-derived labels are optional so the QR renders independently of
  // roster loading.
  readonly courseName?: string | undefined
  readonly sessionName?: string | undefined
  readonly checkedCount?: number | undefined
  readonly totalCount?: number | undefined
  readonly refreshing: boolean
  readonly onClose: () => void
  readonly onRefresh: () => void
}

function useCountdownSeconds(expiresAt: string | undefined): number | null {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (expiresAt === undefined) {
      return
    }
    const interval = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(interval)
  }, [expiresAt])
  if (expiresAt === undefined) {
    return null
  }
  return Math.max(0, Math.floor((new Date(expiresAt).getTime() - now) / 1000))
}

export function QrDialog({
  open,
  qrUrl,
  expiresAt,
  errorMessage,
  warningMessage,
  courseName,
  sessionName,
  checkedCount,
  totalCount,
  refreshing,
  onClose,
  onRefresh,
}: QrDialogProps) {
  const secondsLeft = useCountdownSeconds(expiresAt)
  const hasQr = qrUrl !== undefined && qrUrl !== ''
  // A QR whose expiry is missing or already past may be rejected when scanned.
  // Suppress the warning while a refresh is in flight so it does not flash.
  const showStaleWarning = hasQr && (secondsLeft === null || secondsLeft === 0) && !refreshing
  const blockerMessage = errorMessage ?? warningMessage

  return (
    <Dialog
      {...(courseName === undefined
        ? {}
        : { description: `${courseName}, ${sessionName ?? 'Session'}` })}
      onClose={onClose}
      open={open}
      title="Student check-in QR code"
    >
      <div className="qr-dialog">
        {!hasQr && blockerMessage !== undefined ? (
          <div className="qr-dialog__blocked" role="alert">
            <p>{blockerMessage}</p>
            <p>Refresh to try again once the session recovers.</p>
          </div>
        ) : !hasQr ? (
          <div className="qr-dialog__pending" role="status">
            <Spinner size="lg" />
            <p>Generating a fresh QR code…</p>
          </div>
        ) : (
          <img
            alt={`QR code for ${sessionName ?? 'this session'}`}
            className="qr-dialog__image"
            src={qrUrl}
          />
        )}
        {secondsLeft !== null ? (
          <p
            className={`qr-dialog__expiry${secondsLeft <= 10 ? ' qr-dialog__expiry--urgent' : ''}`}
            role="timer"
          >
            Expires in: {secondsLeft}s
          </p>
        ) : null}
        {showStaleWarning ? (
          <p className="qr-dialog__warning" role="alert">
            This QR code may no longer be valid. Refresh to get the latest code.
          </p>
        ) : null}
        {checkedCount !== undefined && totalCount !== undefined ? (
          <p className="qr-dialog__count">
            <strong>{checkedCount}</strong> of {totalCount} students checked in
          </p>
        ) : null}
        <div className="cluster">
          <Button loading={refreshing} onClick={onRefresh}>Refresh QR code</Button>
          <Button variant="ghost" onClick={onClose}>Close</Button>
        </div>
      </div>
    </Dialog>
  )
}
