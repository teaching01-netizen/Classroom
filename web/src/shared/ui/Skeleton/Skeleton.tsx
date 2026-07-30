type SkeletonProps = {
  readonly lines?: number
}

export function Skeleton({ lines = 3 }: SkeletonProps) {
  return (
    <div aria-busy="true" aria-label="Loading" className="ui-skeleton" role="status">
      {Array.from({ length: lines }, (_, index) => (
        <span key={index} />
      ))}
    </div>
  )
}
