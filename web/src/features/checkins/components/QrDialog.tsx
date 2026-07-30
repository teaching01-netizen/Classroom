import { Dialog } from '@/shared/ui/Dialog'
import { Button } from '@/shared/ui/Button'
import { Spinner } from '@/shared/ui/Spinner'

type QrDialogProps = {
  readonly open: boolean
  readonly qrUrl?: string | undefined
  readonly courseName: string
  readonly sessionName: string
  readonly checkedCount: number
  readonly totalCount: number
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
      description={`${courseName}, ${sessionName}`}
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
          <img alt={`QR code for ${sessionName}`} className="qr-dialog__image" src={qrUrl} />
        )}
        <p className="qr-dialog__count">
          <strong>{checkedCount}</strong> of {totalCount} students checked in
        </p>
        <div className="cluster">
          <Button loading={refreshing} onClick={onRefresh}>Refresh QR code</Button>
          <Button variant="ghost" onClick={onClose}>Close</Button>
        </div>
      </div>
    </Dialog>
  )
}
