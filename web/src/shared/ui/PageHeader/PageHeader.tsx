import type { ReactNode } from 'react'

type PageHeaderProps = {
  readonly eyebrow?: string
  readonly title: string
  readonly description?: string
  readonly actions?: ReactNode
}

export function PageHeader({ eyebrow, title, description, actions }: PageHeaderProps) {
  return (
    <header className="ui-page-header">
      <div>
        {eyebrow !== undefined && <p className="ui-page-header__eyebrow">{eyebrow}</p>}
        <h2>{title}</h2>
        {description !== undefined && <p>{description}</p>}
      </div>
      {actions !== undefined && <div className="ui-page-header__actions">{actions}</div>}
    </header>
  )
}
