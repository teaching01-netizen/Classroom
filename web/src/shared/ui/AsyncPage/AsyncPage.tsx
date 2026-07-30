import type { ReactNode } from 'react'
import { Button } from '@/shared/ui/Button'
import { EmptyState } from '@/shared/ui/EmptyState'
import { ErrorState } from '@/shared/ui/ErrorState'
import { Skeleton } from '@/shared/ui/Skeleton'

type AsyncPageProps = {
  readonly pending: boolean
  readonly fetching?: boolean
  readonly error: string | null
  readonly empty: boolean
  readonly emptyTitle: string
  readonly emptyDescription: string
  readonly onRetry?: () => void
  readonly children: ReactNode
}

export function AsyncPage({
  pending,
  fetching = false,
  error,
  empty,
  emptyTitle,
  emptyDescription,
  onRetry,
  children,
}: AsyncPageProps) {
  if (pending) {
    return <Skeleton lines={6} />
  }
  if (error !== null) {
    return (
      <ErrorState message={error}>
        {onRetry !== undefined && <Button onClick={onRetry}>Retry</Button>}
      </ErrorState>
    )
  }
  if (empty) {
    return <EmptyState title={emptyTitle} description={emptyDescription} />
  }
  return (
    <>
      {fetching && (
        <div aria-live="polite" className="sync-indicator" role="status">
          Synchronizing…
        </div>
      )}
      {children}
    </>
  )
}
