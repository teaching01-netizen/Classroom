import { Link } from 'react-router-dom'
import { EmptyState } from '@/shared/ui/EmptyState'

export function NotFoundRoute() {
  return (
    <EmptyState
      title="Page not found"
      description="The page may have moved or the link may be incomplete."
    >
      <Link className="button-link" to="/">Return to dashboard</Link>
    </EmptyState>
  )
}
