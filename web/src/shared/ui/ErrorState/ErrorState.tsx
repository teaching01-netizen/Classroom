import type { ReactNode } from 'react'

type ErrorStateProps = {
  readonly title?: string
  readonly message: string
  readonly children?: ReactNode
}

export function ErrorState({
  title = 'Unable to load this page',
  message,
  children,
}: ErrorStateProps) {
  return (
    <section className="ui-error-state" role="alert">
      <h2>{title}</h2>
      <p>{message}</p>
      {children !== undefined && <div className="ui-error-state__actions">{children}</div>}
    </section>
  )
}
