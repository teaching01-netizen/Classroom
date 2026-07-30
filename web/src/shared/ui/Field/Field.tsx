import { useId, type ReactNode } from 'react'

type FieldProps = {
  readonly label: string
  readonly description?: string
  readonly error?: string
  readonly required?: boolean
  readonly children: (props: {
    readonly id: string
    readonly 'aria-describedby': string | undefined
    readonly 'aria-invalid': boolean | undefined
    readonly required: boolean
  }) => ReactNode
}

export function Field({
  label,
  description,
  error,
  required = false,
  children,
}: FieldProps) {
  const id = useId()
  const descriptionId = description === undefined ? undefined : `${id}-description`
  const errorId = error === undefined ? undefined : `${id}-error`
  const describedBy = [descriptionId, errorId].filter(Boolean).join(' ') || undefined

  return (
    <div className="ui-field">
      <label htmlFor={id}>
        {label}
        {required && <span aria-hidden="true"> *</span>}
      </label>
      {children({
        id,
        'aria-describedby': describedBy,
        'aria-invalid': error === undefined ? undefined : true,
        required,
      })}
      {description !== undefined && <p id={descriptionId}>{description}</p>}
      {error !== undefined && <p className="ui-field__error" id={errorId}>{error}</p>}
    </div>
  )
}
