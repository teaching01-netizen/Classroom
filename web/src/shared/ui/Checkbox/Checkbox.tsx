import { forwardRef, type InputHTMLAttributes } from 'react'

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> & {
  readonly label: string
}

export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(
  function Checkbox({ label, className, ...props }, ref) {
    return (
      <label className={['ui-checkbox', className].filter(Boolean).join(' ')}>
        <input {...props} ref={ref} type="checkbox" />
        <span>{label}</span>
      </label>
    )
  },
)
