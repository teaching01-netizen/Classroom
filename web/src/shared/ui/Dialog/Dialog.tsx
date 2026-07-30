import { useEffect, useId, useRef, type ReactNode } from 'react'
import { IconButton } from '@/shared/ui/IconButton'

type DialogProps = {
  readonly open: boolean
  readonly title: string
  readonly description?: string
  readonly dismissible?: boolean
  readonly onClose: () => void
  readonly children: ReactNode
}

export function Dialog({
  open,
  title,
  description,
  dismissible = true,
  onClose,
  children,
}: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    const dialog = dialogRef.current
    if (dialog === null) {
      return
    }
    if (open && !dialog.open) {
      dialog.showModal()
      const firstFocusable = dialog.querySelector<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
      )
      firstFocusable?.focus()
    } else if (!open && dialog.open) {
      dialog.close()
    }
  }, [open])

  return (
    <dialog
      aria-describedby={description === undefined ? undefined : descriptionId}
      aria-labelledby={titleId}
      className="ui-dialog"
      onCancel={(event) => {
        if (!dismissible) {
          event.preventDefault()
          return
        }
        onClose()
      }}
      onClick={(event) => {
        if (dismissible && event.target === event.currentTarget) {
          onClose()
        }
      }}
      ref={dialogRef}
    >
      <div className="ui-dialog__surface">
        <div className="ui-dialog__header">
          <div>
            <h2 id={titleId}>{title}</h2>
            {description !== undefined && <p id={descriptionId}>{description}</p>}
          </div>
          {dismissible && (
            <IconButton label="Close dialog" onClick={onClose}>×</IconButton>
          )}
        </div>
        <div className="ui-dialog__body">{children}</div>
      </div>
    </dialog>
  )
}
