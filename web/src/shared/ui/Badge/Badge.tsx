import type { ReactNode } from 'react'

export type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

type BadgeProps = {
  readonly tone?: BadgeTone
  readonly children: ReactNode
}

export function Badge({ tone = 'neutral', children }: BadgeProps) {
  return <span className="ui-badge" data-tone={tone}>{children}</span>
}
