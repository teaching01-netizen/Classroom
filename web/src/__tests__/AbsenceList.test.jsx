import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AbsenceList } from '../components/dashboard/AbsenceList';

const sessions = [
  { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', date: '2026-06-10', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', date: '2026-06-17', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's3', sessionNumber: 1, name: 'Wk 1', date: '2026-06-11', courseId: 'c2', courseName: 'English Adv', status: 'done' },
];

const students = [
  {
    studentId: 'stu-1',
    name: 'Alice',
    nickname: 'Ali',
    school: 'Concord',
    avatarUrl: '',
    attendedSessions: 1,
    totalSessions: 3,
    attendanceRate: 0.33,
    atRisk: true,
    courses: [
      { courseId: 'c1', courseName: 'SAT Math', totalSessions: 2, attendedSessions: 1, rate: 0.5, absences: 1, atRisk: false },
      { courseId: 'c2', courseName: 'English Adv', totalSessions: 1, attendedSessions: 0, rate: 0, absences: 1, atRisk: true },
    ],
    perSession: [
      { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: false, status: 'absent' },
      { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
      { sessionId: 's3', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'absent' },
    ],
  },
  {
    studentId: 'stu-2',
    name: 'Bob',
    nickname: '',
    school: 'Satit',
    avatarUrl: '',
    attendedSessions: 3,
    totalSessions: 3,
    attendanceRate: 1.0,
    atRisk: false,
    courses: [
      { courseId: 'c1', courseName: 'SAT Math', totalSessions: 2, attendedSessions: 2, rate: 1.0, absences: 0, atRisk: false },
      { courseId: 'c2', courseName: 'English Adv', totalSessions: 1, attendedSessions: 1, rate: 1.0, absences: 0, atRisk: false },
    ],
    perSession: [
      { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
      { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
      { sessionId: 's3', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    ],
  },
];

describe('AbsenceList', () => {
  it('renders all student cards', () => {
    render(<AbsenceList students={students} sessions={sessions} />);
    expect(screen.getByText('Alice')).toBeTruthy();
    expect(screen.getByText('Bob')).toBeTruthy();
  });

  it('shows empty message when students array is empty', () => {
    render(<AbsenceList students={[]} sessions={[]} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });

  it('shows empty message when students is null', () => {
    render(<AbsenceList students={null} sessions={[]} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });
});
