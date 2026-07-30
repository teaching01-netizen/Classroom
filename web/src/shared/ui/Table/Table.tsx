import type { HTMLAttributes, TableHTMLAttributes } from 'react'

export function TableContainer(props: HTMLAttributes<HTMLDivElement>) {
  return <div {...props} className={['ui-table-container', props.className].filter(Boolean).join(' ')} />
}

export function Table(props: TableHTMLAttributes<HTMLTableElement>) {
  return <table {...props} className={['ui-table', props.className].filter(Boolean).join(' ')} />
}
