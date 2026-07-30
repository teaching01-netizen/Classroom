type AvatarProps = {
  readonly name: string
  readonly src?: string
  readonly size?: 'sm' | 'md'
}

export function Avatar({ name, src, size = 'md' }: AvatarProps) {
  const initials = name
    .split(/\s+/)
    .map((part) => part[0] ?? '')
    .join('')
    .slice(0, 2)
    .toUpperCase()

  return src === undefined || src === ''
    ? <span aria-hidden="true" className="ui-avatar" data-size={size}>{initials}</span>
    : <img alt="" className="ui-avatar" data-size={size} src={src} />
}
