import type { ReactNode } from 'react'

export type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

type BadgeProps = {
  readonly tone?: BadgeTone
  readonly title?: string | undefined
  readonly children: ReactNode
}

export function Badge({ tone = 'neutral', title, children }: BadgeProps) {
  return (
    <span className="ui-badge" data-tone={tone} title={title}>
      {children}
    </span>
  )
}
