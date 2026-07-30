type Stat = {
  readonly label: string
  readonly value: string | number
  readonly tone?: 'default' | 'positive' | 'warning' | 'danger'
}

type StatsGridProps = {
  readonly stats: readonly Stat[]
}

export function StatsGrid({ stats }: StatsGridProps) {
  return (
    <dl className="ui-stats-grid">
      {stats.map((stat) => (
        <div data-tone={stat.tone ?? 'default'} key={stat.label}>
          <dt>{stat.label}</dt>
          <dd>{stat.value}</dd>
        </div>
      ))}
    </dl>
  )
}
