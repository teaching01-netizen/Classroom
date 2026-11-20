import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { StudentCheckin, StudentId } from '../api/checkin.schemas'
import { StudentCheckinTable } from './StudentCheckinTable'

// Three fixtures chosen so a wrong gate cannot pass:
//   Alice   checked in, scored, eligible
//   Sam     checked out, scored (1 pt), NOT eligible  -> kills `points > 0`
//   Jordan  checked out, zero points, eligible        -> kills `checked_in`
function makeStudent(
  overrides: Partial<StudentCheckin> & Pick<StudentCheckin, 'student_id' | 'name'>,
): StudentCheckin {
  return {
    nickname: '',
    school: '',
    avatar_url: '',
    checked_in: false,
    checked_in_at: null,
    participation_points: 0,
    can_give_point: false,
    ...overrides,
  }
}

const alice = makeStudent({
  student_id: 's-alice' as StudentId,
  name: 'Alice Chen',
  checked_in: true,
  can_give_point: true,
  participation_points: 4,
})
const sam = makeStudent({
  student_id: 's-sam' as StudentId,
  name: 'Sam Rivera',
  checked_in: false,
  can_give_point: false,
  participation_points: 1,
})
const jordan = makeStudent({
  student_id: 's-jordan' as StudentId,
  name: 'Jordan Lee',
  checked_in: false,
  can_give_point: true,
  participation_points: 0,
})
const roster = [alice, sam, jordan] as const

function bodyRows() {
  return screen.getAllByRole('row').slice(1)
}

const noop = () => undefined

describe('StudentCheckinTable per-row give-points contract', () => {
  it('R1 renders one body row per student, in order', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const rows = bodyRows()

    expect(rows).toHaveLength(roster.length)
    expect(within(rows[0]!).getByText('s-alice')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('s-sam')).toBeInTheDocument()
    expect(within(rows[2]!).getByText('s-jordan')).toBeInTheDocument()
  })

  it('R2/R2prime renders a control exactly when can_give_point is true', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const rows = bodyRows()
    roster.forEach((member, index) => {
      const controls = within(rows[index]!).queryAllByRole('button', {
        name: /^Give points to /,
      })
      expect(controls).toHaveLength(member.can_give_point ? 1 : 0)
    })
  })

  it('R2prime leaves the ineligible cell without any button', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const samRow = bodyRows()[1]!
    const giveCell = within(samRow).getAllByRole('cell')[4]!

    expect(within(giveCell).queryAllByRole('button')).toHaveLength(0)
  })

  it('R3 names every control after its own student, uniquely', () => {
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={noop} />)

    const names = screen
      .getAllByRole('button', { name: /^Give points to / })
      .map((button) => button.getAttribute('aria-label'))

    expect(names).toEqual(['Give points to Alice Chen', 'Give points to Jordan Lee'])
    expect(new Set(names).size).toBe(names.length)
  })

  it('R4 passes the row student object, not a row index', () => {
    const onGivePoints = vi.fn()
    render(<StudentCheckinTable students={roster} onToggle={noop} onGivePoints={onGivePoints} />)

    fireEvent.click(
      within(bodyRows()[2]!).getByRole('button', { name: 'Give points to Jordan Lee' }),
    )

    expect(onGivePoints).toHaveBeenCalledTimes(1)
    expect(onGivePoints).toHaveBeenCalledWith(jordan)
  })

  it('R5 marks only the pending row busy and disabled', () => {
    render(
      <StudentCheckinTable
        students={roster}
        pendingPointsStudentId={'s-jordan' as StudentId}
        onToggle={noop}
        onGivePoints={noop}
      />,
    )

    const pending = screen.getByRole('button', { name: 'Give points to Jordan Lee' })
    const sibling = screen.getByRole('button', { name: 'Give points to Alice Chen' })

    expect(pending).toBeDisabled()
    expect(pending).toHaveAttribute('aria-busy', 'true')
    expect(sibling).toBeEnabled()
  })
})
