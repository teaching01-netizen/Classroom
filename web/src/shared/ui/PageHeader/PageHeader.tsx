import type { ReactNode } from 'react'

type PageHeaderProps = {
  readonly eyebrow?: string
  readonly title: string
  readonly description?: string
  // status sits directly under the description: a compact status line about
  // the page's data (e.g. a freshness badge), distinct from actions.
  readonly status?: ReactNode
  readonly actions?: ReactNode
}

export function PageHeader({ eyebrow, title, description, status, actions }: PageHeaderProps) {
  return (
    <header className="ui-page-header">
      <div>
        {eyebrow !== undefined && <p className="ui-page-header__eyebrow">{eyebrow}</p>}
        <h2>{title}</h2>
        {description !== undefined && <p>{description}</p>}
        {status !== undefined && <p className="ui-page-header__status">{status}</p>}
      </div>
      {actions !== undefined && <div className="ui-page-header__actions">{actions}</div>}
    </header>
  )
}
