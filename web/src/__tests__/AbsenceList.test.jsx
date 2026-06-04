import React from 'react';
import { render, screen, within } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AbsenceList } from '../components/dashboard/AbsenceList';

const sessions = [
  { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', date: '2026-06-10', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', date: '2026-06-17', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's3', sessionNumber: 1, name: 'Wk 1', date: '2026-06-11', courseId: 'c2', courseName: 'English Adv', status: 'done' },
];

const alice = {
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

const carol = {
  studentId: 'stu-3',
  name: 'Carol',
  nickname: '',
  school: 'Satit',
  avatarUrl: '',
  attendedSessions: 1,
  totalSessions: 2,
  attendanceRate: 0.5,
  atRisk: false,
  courses: [
    { courseId: 'c1', courseName: 'SAT Math', totalSessions: 2, attendedSessions: 1, rate: 0.5, absences: 1, atRisk: false },
  ],
  perSession: [
    { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: false, status: 'absent' },
  ],
};

const bob = {
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
};

describe('AbsenceList (table style)', () => {
  it('groups absences by course as section headers', () => {
    render(<AbsenceList students={[alice, bob]} sessions={sessions} />);
    const mathHeader = screen.getByText('SAT Math');
    const englishHeader = screen.getByText('English Adv');
    expect(mathHeader).toBeTruthy();
    expect(englishHeader).toBeTruthy();
  });

  it('shows one row per (student, course) pair with absences', () => {
    render(<AbsenceList students={[alice, carol]} sessions={sessions} />);
    const tableRows = screen.getAllByRole('row');
    const dataRows = tableRows.filter((row) => row.querySelector('td'));
    expect(dataRows.length).toBe(3);
  });

  it('does not include students with zero absences', () => {
    render(<AbsenceList students={[alice, bob, carol]} sessions={sessions} />);
    expect(screen.queryByText('Bob')).toBeNull();
  });

  it('shows the absence count per (student, course) row', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    const tableRows = screen.getAllByRole('row');
    const dataRows = tableRows.filter((row) => row.querySelector('td'));
    expect(dataRows.length).toBe(2);
    const counts = screen.getAllByText('1');
    expect(counts.length).toBeGreaterThanOrEqual(2);
  });

  it('shows the latest absence date for each row', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    expect(screen.getByText('2026-06-10')).toBeTruthy();
    expect(screen.getByText('2026-06-11')).toBeTruthy();
  });

  it('shows at-risk badge on rows where the student is at risk', () => {
    render(<AbsenceList students={[alice, carol]} sessions={sessions} />);
    const badges = screen.getAllByText('AT RISK');
    expect(badges.length).toBe(2);
  });

  it('sorts courses alphabetically', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    const headers = screen.getAllByRole('heading', { level: 3 });
    const headerTexts = headers.map((h) => h.textContent);
    const mathIndex = headerTexts.findIndex((t) => t.includes('SAT Math'));
    const englishIndex = headerTexts.findIndex((t) => t.includes('English Adv'));
    expect(englishIndex).toBeLessThan(mathIndex);
  });

  it('shows empty message when students array is empty', () => {
    render(<AbsenceList students={[]} sessions={sessions} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });

  it('shows empty message when students is null', () => {
    render(<AbsenceList students={null} sessions={sessions} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });

  it('uses a real <table> element for accessibility', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    expect(screen.getAllByRole('table').length).toBeGreaterThan(0);
  });
});
