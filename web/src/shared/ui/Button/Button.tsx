import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Spinner } from '@/shared/ui/Spinner'

export type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost'
export type ButtonSize = 'sm' | 'md' | 'lg'

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  readonly variant?: ButtonVariant
  readonly size?: ButtonSize
  readonly loading?: boolean
  readonly children: ReactNode
}

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  disabled = false,
  className,
  children,
  ...props
}: ButtonProps) {
  const classes = ['ui-button', className].filter(Boolean).join(' ')
  return (
    <button
      {...props}
      aria-busy={loading || undefined}
      className={classes}
      data-size={size}
      data-variant={variant}
      disabled={disabled || loading}
    >
      {loading && <Spinner size="sm" />}
      <span>{children}</span>
    </button>
  )
}
