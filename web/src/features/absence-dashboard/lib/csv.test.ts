import { describe, expect, it } from 'vitest'
import { courseIdSchema } from '@/features/courses'
import { studentIdSchema } from '@/features/checkins'
import { sessionIdSchema } from '@/features/sessions'
import type { AbsenceDashboard } from '../api/absence.schemas'
import { absenceDashboardToCsv } from './csv'

const c1 = courseIdSchema.parse('c1')
const c2 = courseIdSchema.parse('c2')
const s1 = sessionIdSchema.parse('s1')
const s2 = sessionIdSchema.parse('s2')
const s3 = sessionIdSchema.parse('s3')

const report: AbsenceDashboard = {
  generatedAt: '2026-06-20T10:00:00Z',
  totalStudents: 2,
  totalCourses: 2,
  avgAttendanceRate: 0.4,
  atRiskCount: 1,
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
      studentId: studentIdSchema.parse('W1001'), name: 'Alice Chen', nickname: 'Alice', school: 'Intensive', avatarUrl: '',
      attendedSessions: 1, totalSessions: 3, attendanceRate: 0.33, atRisk: true,
      courses: [
        { courseId: c1, courseName: 'SAT Math', totalSessions: 2, attendedSessions: 1, rate: 0.5, absences: 1, atRisk: false },
        { courseId: c2, courseName: 'English Adv', totalSessions: 1, attendedSessions: 0, rate: 0, absences: 1, atRisk: true },
      ],
      perSession: [],
    },
    {
      studentId: studentIdSchema.parse('W1002'), name: 'Bob, Smith', nickname: '', school: 'Intensive', avatarUrl: '',
      attendedSessions: 0, totalSessions: 2, attendanceRate: 0, atRisk: true,
      courses: [
        { courseId: c1, courseName: 'SAT Math', totalSessions: 2, attendedSessions: 0, rate: 0, absences: 2, atRisk: true },
      ],
      perSession: [],
    },
  ],
}

describe('absenceDashboardToCsv', () => {
  it('emits a BOM and a per-course header with absences, sessions, and rate', () => {
    const csv = absenceDashboardToCsv(report)
    expect(csv.startsWith('\ufeff')).toBe(true)
    expect(csv.slice(1).split('\n')[0]).toBe(
      'WCode,Name,Nickname,School,'
        + 'SAT Math absences,SAT Math sessions,SAT Math absence rate,'
        + 'English Adv absences,English Adv sessions,English Adv absence rate,'
        + 'Total absences,Attended,Total sessions,Rate',
    )
  })

  it('writes per-course absences, total sessions, and absence rate for each student', () => {
    const lines = absenceDashboardToCsv(report).split('\n')
    expect(lines[1]).toBe('W1001,Alice Chen,Alice,Intensive,1,2,50%,1,1,100%,2,1,3,33%')
  })

  it('leaves the course cells empty when the student does not study that course', () => {
    const lines = absenceDashboardToCsv(report).split('\n')
    expect(lines[2]).toBe('W1002,"Bob, Smith",,Intensive,2,2,100%,,,,2,0,2,0%')
  })
})
