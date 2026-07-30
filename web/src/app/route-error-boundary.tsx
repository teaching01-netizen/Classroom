import { isRouteErrorResponse, Link, useRouteError } from 'react-router-dom'
import { ErrorState } from '@/shared/ui/ErrorState'
import { getErrorMessage } from '@/shared/lib/errors'

export function RootRouteErrorBoundary() {
  const error = useRouteError()
  const title = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : 'Something went wrong'
  const message = isRouteErrorResponse(error)
    ? typeof error.data === 'string'
      ? error.data
      : 'This page could not be loaded.'
    : getErrorMessage(error)

  return (
    <main className="route-error-page">
      <ErrorState title={title} message={message}>
        <Link className="button-link" to="/">Return to dashboard</Link>
      </ErrorState>
    </main>
  )
}
