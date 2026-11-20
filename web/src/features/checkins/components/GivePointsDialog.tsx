import { useEffect, useState } from 'react'
import { Button } from '@/shared/ui/Button'
import { Dialog } from '@/shared/ui/Dialog'
import { Field } from '@/shared/ui/Field'
import { Input } from '@/shared/ui/Input'

const MIN_POINTS = 1
const MAX_POINTS = 10

type GivePointsDialogProps = {
  readonly open: boolean
  readonly studentName: string
  readonly pending: boolean
  readonly errorMessage?: string | undefined
  readonly onClose: () => void
  readonly onSubmit: (points: number) => void
}

export function GivePointsDialog({
  open,
  studentName,
  pending,
  errorMessage,
  onClose,
  onSubmit,
}: GivePointsDialogProps) {
  const [value, setValue] = useState('1')
  const [validationError, setValidationError] = useState<string>()

  useEffect(() => {
    if (open) {
      setValue('1')
      setValidationError(undefined)
    }
  }, [open])

  const submit = () => {
    const parsed = Number(value)
    if (!Number.isSafeInteger(parsed) || parsed < MIN_POINTS || parsed > MAX_POINTS) {
      setValidationError(`Points must be between ${MIN_POINTS} and ${MAX_POINTS}.`)
      return
    }
    setValidationError(undefined)
    onSubmit(parsed)
  }

  return (
    <Dialog
      open={open}
      title="Give participation points"
      description={`Award points to ${studentName} in Humanix. This writes immediately.`}
      onClose={onClose}
      dismissible={!pending}
    >
      <form
        className="give-points"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          submit()
        }}
      >
        <Field
          label="Points"
          required
          {...(validationError === undefined ? {} : { error: validationError })}
        >
          {(fieldProps) => (
            <Input
              {...fieldProps}
              type="number"
              min={MIN_POINTS}
              max={MAX_POINTS}
              step={1}
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          )}
        </Field>
        {errorMessage !== undefined && (
          <p className="give-points__error" role="alert">{errorMessage}</p>
        )}
        <div className="give-points__actions">
          <Button type="button" variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={pending}>
            Award points
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
