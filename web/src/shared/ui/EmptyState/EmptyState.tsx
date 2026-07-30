import type { ReactNode } from 'react'

type EmptyStateProps = {
  readonly title: string
  readonly description: string
  readonly children?: ReactNode
}

export function EmptyState({ title, description, children }: EmptyStateProps) {
  return (
    <section className="ui-empty-state">
      <div className="ui-empty-state__icon" aria-hidden="true">◇</div>
      <h2>{title}</h2>
      <p>{description}</p>
      {children !== undefined && <div className="ui-empty-state__actions">{children}</div>}
    </section>
  )
}
