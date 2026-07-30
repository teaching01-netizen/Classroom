import { Link } from 'react-router-dom'

type BackLinkProps = {
  readonly to: string
  readonly children: string
}

export function BackLink({ to, children }: BackLinkProps) {
  return <Link className="ui-back-link" to={to}>← {children}</Link>
}
