import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { courseIdSchema } from '@/features/courses'
import { studentIdSchema } from '@/features/checkins'
import { sessionIdSchema } from '@/features/sessions'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { AbsenceResults } from './AbsenceResults'

const c1 = courseIdSchema.parse('c1')
const c2 = courseIdSchema.parse('c2')
const s1 = sessionIdSchema.parse('s1')
const s2 = sessionIdSchema.parse('s2')
const s3 = sessionIdSchema.parse('s3')
const aliceId = studentIdSchema.parse('W1001')
const bobId = studentIdSchema.parse('W1002')
const carolId = studentIdSchema.parse('W1003')
const dianeId = studentIdSchema.parse('W1004')

const report: AbsenceDashboard = {
  generatedAt: '2026-06-20T10:00:00Z',
  totalStudents: 4,
  totalCourses: 2,
  avgAttendanceRate: 0.17,
  atRiskCount: 3,
  topAtRisk: [],
  sessions: [
    {
      sessionId: s1, sessionNumber: 1, name: 'Wk 1', date: '2026-06-10',
      courseId: c1, courseName: 'SAT Math', checkedInCount: 1, totalStudents: 2, status: 'done',
    },
    {
      sessionId: s2, sessionNumber: 2, name: 'Wk 2', date: '2026-06-17',
      courseId: c1, courseName: 'SAT Math', checkedInCount: 0, totalStudents: 2, status: 'done',
    },
    {
      sessionId: s3, sessionNumber: 1, name: 'Wk 1', date: '2026-06-11',
      courseId: c2, courseName: 'English Adv', checkedInCount: 0, totalStudents: 2, status: 'done',
    },
  ],
  students: [
    {
      studentId: aliceId, name: 'Alice Chen', nickname: 'Alice', school: 'Intensive', avatarUrl: '',
      attendedSessions: 1, totalSessions: 3, attendanceRate: 0.33, atRisk: true,
      courses: [
        { courseId: c1, courseName: 'SAT Math', totalSessions: 2, attendedSessions: 1, rate: 0.5, absences: 1, atRisk: false },
        { courseId: c2, courseName: 'English Adv', totalSessions: 1, attendedSessions: 0, rate: 0, absences: 1, atRisk: true },
      ],
      perSession: [
        { sessionId: s1, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
        { sessionId: s2, sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: false, status: 'absent' },
        { sessionId: s3, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'absent' },
      ],
    },
    {
      studentId: bobId, name: 'Bob Smith', nickname: '', school: 'Intensive', avatarUrl: '',
      attendedSessions: 0, totalSessions: 2, attendanceRate: 0, atRisk: true,
      courses: [
        { courseId: c1, courseName: 'SAT Math', totalSessions: 2, attendedSessions: 0, rate: 0, absences: 2, atRisk: true },
      ],
      perSession: [
        { sessionId: s1, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: false, status: 'absent' },
        { sessionId: s2, sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: false, status: 'absent' },
        { sessionId: s3, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'not_started' },
      ],
    },
    {
      studentId: carolId, name: 'Carol Nguyen', nickname: '', school: 'Intensive', avatarUrl: '',
      attendedSessions: 0, totalSessions: 1, attendanceRate: 0, atRisk: true,
      courses: [
        { courseId: c2, courseName: 'English Adv', totalSessions: 1, attendedSessions: 0, rate: 0, absences: 1, atRisk: true },
      ],
      perSession: [
        { sessionId: s1, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: false, status: 'not_started' },
        { sessionId: s2, sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: false, status: 'not_started' },
        { sessionId: s3, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'absent' },
      ],
    },
    {
      studentId: dianeId, name: 'Diane Liu', nickname: '', school: 'Intensive', avatarUrl: '',
      attendedSessions: 2, totalSessions: 2, attendanceRate: 1, atRisk: false,
      courses: [
        { courseId: c1, courseName: 'SAT Math', totalSessions: 2, attendedSessions: 2, rate: 1, absences: 0, atRisk: false },
      ],
      perSession: [
        { sessionId: s1, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
        { sessionId: s2, sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
        { sessionId: s3, sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'not_started' },
      ],
    },
  ],
}

describe('AbsenceResults', () => {
  it('shows the absence count per course for every student', () => {
    render(<AbsenceResults report={report} />)
    expect(screen.getByTitle('SAT Math: 1 of 2 sessions absent')).toBeTruthy()
    expect(screen.getByTitle('SAT Math: 2 of 2 sessions absent')).toBeTruthy()
    // Alice and Carol each have 1 absence in English Adv.
    expect(screen.getAllByTitle('English Adv: 1 of 1 sessions absent')).toHaveLength(2)
  })

  it('shows a dash for courses the student does not study', () => {
    render(<AbsenceResults report={report} />)
    const rows = screen.getAllByRole('row')
    const bobRow = rows[2]!
    const carolRow = rows[3]!
    expect(within(bobRow).getByText('—')).toBeTruthy()
    expect(within(carolRow).getByText('—')).toBeTruthy()
    expect(within(bobRow).queryByText(/English Adv/)).toBeNull()
    expect(within(carolRow).queryByText(/SAT Math/)).toBeNull()
  })

  it('shows the absence rate and total sessions per course', () => {
    render(<AbsenceResults report={report} />)
    const rows = screen.getAllByRole('row')
    const aliceRow = rows[1]!
    const bobRow = rows[2]!
    const carolRow = rows[3]!
    const dianeRow = rows[4]!
    // Alice: SAT Math 1 of 2 (50% absent), English Adv 1 of 1 (100% absent)
    expect(within(aliceRow).getByText('1/2')).toBeTruthy()
    expect(within(aliceRow).getByText('50% absent')).toBeTruthy()
    expect(within(aliceRow).getByText('1/1')).toBeTruthy()
    expect(within(aliceRow).getByText('100% absent')).toBeTruthy()
    // Bob: SAT Math 2 of 2 (100% absent)
    expect(within(bobRow).getByText('2/2')).toBeTruthy()
    expect(within(bobRow).getByText('100% absent')).toBeTruthy()
    // Carol: English Adv 1 of 1 (100% absent)
    expect(within(carolRow).getByText('1/1')).toBeTruthy()
    expect(within(carolRow).getByText('100% absent')).toBeTruthy()
    // Diane: SAT Math 0 of 2 (0% absent)
    expect(within(dianeRow).getByText('0/2')).toBeTruthy()
    expect(within(dianeRow).getByText('0% absent')).toBeTruthy()
  })

  it('expands a student to list missed sessions grouped by course', () => {
    render(<AbsenceResults report={report} />)
    fireEvent.click(screen.getByRole('button', { name: 'Show missed sessions for Alice' }))
    // Alice missed SAT Math S2 and English Adv S1.
    expect(screen.getByText('S2 · Wk 2 · 2026-06-17')).toBeTruthy()
    expect(screen.getByText('S1 · Wk 1 · 2026-06-11')).toBeTruthy()
    expect(screen.getAllByText(/1 session missed/)).toHaveLength(2)
  })

  it('groups a student\'s missed sessions under the correct course', () => {
    render(<AbsenceResults report={report} />)
    fireEvent.click(screen.getByRole('button', { name: /Show missed sessions for Bob/ }))
    // Bob misses both SAT Math sessions; English Adv S1 is not started, not missed.
    expect(screen.getByText('S1 · Wk 1 · 2026-06-10')).toBeTruthy()
    expect(screen.getByText(/2 sessions missed/)).toBeTruthy()
    expect(screen.queryByText('S1 · Wk 1 · 2026-06-11')).toBeNull()
  })

  it('collapses the detail row when the chevron is clicked again', () => {
    render(<AbsenceResults report={report} />)
    const toggle = screen.getByRole('button', { name: 'Show missed sessions for Alice' })
    fireEvent.click(toggle)
    expect(screen.getByText('S2 · Wk 2 · 2026-06-17')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Hide missed sessions for Alice' }))
    expect(screen.queryByText('S2 · Wk 2 · 2026-06-17')).toBeNull()
  })

  it('only offers the chevron to students who missed at least one session', () => {
    render(<AbsenceResults report={report} />)
    const toggles = screen.getAllByRole('button', { name: /missed sessions/ })
    expect(toggles).toHaveLength(3)
    expect(toggles[0]).toHaveAttribute('aria-expanded', 'false')
  })

  it('labels the table with course columns, not session columns', () => {
    render(<AbsenceResults report={report} />)
    const header = screen.getAllByRole('columnheader').map((th) => th.textContent)
    expect(header).toEqual(['Student', 'SAT Math', 'English Adv', 'Attended', 'Rate'])
  })

  it('does not show per-session columns at all', () => {
    render(<AbsenceResults report={report} />)
    expect(screen.queryByText('S1')).toBeNull()
    expect(screen.queryByText('S2')).toBeNull()
  })
})
