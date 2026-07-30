import type { ButtonHTMLAttributes, ReactNode } from 'react'

type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> & {
  readonly label: string
  readonly children: ReactNode
}

export function IconButton({ label, className, children, ...props }: IconButtonProps) {
  return (
    <button
      {...props}
      aria-label={label}
      className={['ui-icon-button', className].filter(Boolean).join(' ')}
      type={props.type ?? 'button'}
    >
      {children}
    </button>
  )
}
