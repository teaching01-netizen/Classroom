import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Table, TableContainer } from './Table'

describe('Table', () => {
  it('keeps native table semantics for display data', () => {
    // Given
    const studentName = 'Alice'
    // When
    render(
      <TableContainer>
        <Table>
          <thead><tr><th scope="col">Student</th></tr></thead>
          <tbody><tr><td>{studentName}</td></tr></tbody>
        </Table>
      </TableContainer>,
    )
    // Then
    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Student' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'Alice' })).toBeInTheDocument()
  })
})
