import { Dialog } from '@/shared/ui/Dialog'
import { Button } from '@/shared/ui/Button'
import { Spinner } from '@/shared/ui/Spinner'

type QrDialogProps = {
  readonly open: boolean
  readonly qrUrl?: string | undefined
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

export function QrDialog({
  open,
  qrUrl,
  courseName,
  sessionName,
  checkedCount,
  totalCount,
  refreshing,
  onClose,
  onRefresh,
}: QrDialogProps) {
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
        {qrUrl === undefined || qrUrl === '' ? (
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
