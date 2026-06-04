import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AbsenceStudentCard } from '../components/dashboard/AbsenceStudentCard';

const sessions = [
  { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', date: '2026-06-10', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', date: '2026-06-17', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's3', sessionNumber: 1, name: 'Wk 1', date: '2026-06-11', courseId: 'c2', courseName: 'English Adv', status: 'done' },
];

const student = {
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
};

describe('AbsenceStudentCard', () => {
  it('renders student name', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('Alice')).toBeTruthy();
  });

  it('shows nickname when present', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('(Ali)')).toBeTruthy();
  });

  it('shows at-risk badge when student is at risk', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('AT RISK')).toBeTruthy();
  });

  it('shows school name', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('Concord')).toBeTruthy();
  });

  it('renders absence entry with course name and date', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('SAT Math')).toBeTruthy();
    expect(screen.getByText('2026-06-10')).toBeTruthy();
  });

  it('shows absence count per course', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText('SAT Math')).toBeTruthy();
    expect(screen.getByText('English Adv')).toBeTruthy();
  });

  it('does not show session where student checked in', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.queryByText('Wk 2')).toBeNull();
  });

  it('renders attendance summary', () => {
    render(<AbsenceStudentCard student={student} sessions={sessions} />);
    expect(screen.getByText(/1\/3/)).toBeTruthy();
    expect(screen.getByText(/33%/)).toBeTruthy();
  });

  it('handles student with no absences', () => {
    const perfect = {
      ...student,
      atRisk: false,
      attendedSessions: 3,
      totalSessions: 3,
      attendanceRate: 1.0,
      perSession: [
        { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
        { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
        { sessionId: 's3', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
      ],
    };
    render(<AbsenceStudentCard student={perfect} sessions={sessions} />);
    expect(screen.getByText(/No absences/)).toBeTruthy();
  });
});
