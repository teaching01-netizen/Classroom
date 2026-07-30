import { Button } from '@/shared/ui/Button'

type PaginationProps = {
  readonly page: number
  readonly pageSize: number
  readonly totalItems: number
  readonly onPageChange: (page: number) => void
}

export function Pagination({ page, pageSize, totalItems, onPageChange }: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize))
  if (totalItems <= pageSize) {
    return null
  }
  return (
    <nav aria-label="Pagination" className="ui-pagination">
      <Button
        disabled={page <= 1}
        size="sm"
        onClick={() => onPageChange(page - 1)}
      >
        Previous
      </Button>
      <span aria-live="polite">Page {page} of {totalPages}</span>
      <Button
        disabled={page >= totalPages}
        size="sm"
        onClick={() => onPageChange(page + 1)}
      >
        Next
      </Button>
    </nav>
  )
}
