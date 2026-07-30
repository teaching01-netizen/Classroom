type SpinnerProps = {
  readonly size?: 'sm' | 'md' | 'lg'
  readonly label?: string
}

export function Spinner({ size = 'md', label }: SpinnerProps) {
  return (
    <span
      aria-hidden={label === undefined ? true : undefined}
      aria-label={label}
      className="ui-spinner"
      data-size={size}
      role={label === undefined ? undefined : 'status'}
    />
  )
}
