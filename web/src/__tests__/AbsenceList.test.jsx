import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AbsenceList } from '../components/dashboard/AbsenceList';

const sessions = [
  { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', date: '2026-06-10', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', date: '2026-06-17', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's3', sessionNumber: 3, name: 'Wk 3', date: '2026-06-24', courseId: 'c1', courseName: 'SAT Math', status: 'done' },
  { sessionId: 's4', sessionNumber: 1, name: 'Wk 1', date: '2026-06-11', courseId: 'c2', courseName: 'English Adv', status: 'done' },
];

const alice = {
  studentId: 'stu-1',
  name: 'Alice',
  nickname: 'Ali',
  school: 'Concord',
  avatarUrl: '',
  attendedSessions: 2,
  totalSessions: 4,
  attendanceRate: 0.5,
  atRisk: true,
  courses: [
    { courseId: 'c1', courseName: 'SAT Math', totalSessions: 3, attendedSessions: 1, rate: 0.33, absences: 2, atRisk: true },
    { courseId: 'c2', courseName: 'English Adv', totalSessions: 1, attendedSessions: 0, rate: 0, absences: 1, atRisk: true },
  ],
  perSession: [
    { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: false, status: 'absent' },
    { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: false, status: 'absent' },
    { sessionId: 's3', sessionNumber: 3, sessionName: 'Wk 3', sessionDate: '2026-06-24', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    { sessionId: 's4', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: false, status: 'absent' },
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
  attendedSessions: 4,
  totalSessions: 4,
  attendanceRate: 1.0,
  atRisk: false,
  courses: [
    { courseId: 'c1', courseName: 'SAT Math', totalSessions: 3, attendedSessions: 3, rate: 1.0, absences: 0, atRisk: false },
    { courseId: 'c2', courseName: 'English Adv', totalSessions: 1, attendedSessions: 1, rate: 1.0, absences: 0, atRisk: false },
  ],
  perSession: [
    { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-10', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionDate: '2026-06-17', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    { sessionId: 's3', sessionNumber: 3, sessionName: 'Wk 3', sessionDate: '2026-06-24', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
    { sessionId: 's4', sessionNumber: 1, sessionName: 'Wk 1', sessionDate: '2026-06-11', sessionStatus: 'done', checkedIn: true, status: 'checked_in' },
  ],
};

describe('AbsenceList (student summary + drill-down)', () => {
  it('groups by student — one summary row per student with absences', () => {
    render(<AbsenceList students={[alice, carol]} sessions={sessions} />);
    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBe(2);
  });

  it('does not include students with zero absences in the summary', () => {
    render(<AbsenceList students={[alice, bob, carol]} sessions={sessions} />);
    expect(screen.queryByText('Bob')).toBeNull();
  });

  it('summary shows total absence count and course names', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    expect(screen.getByText(/3 absences?/)).toBeTruthy();
    expect(screen.getByText(/SAT Math/)).toBeTruthy();
    expect(screen.getByText(/English Adv/)).toBeTruthy();
  });

  it('summary shows at-risk badge when student is at risk', () => {
    render(<AbsenceList students={[alice, carol]} sessions={sessions} />);
    const badges = screen.getAllByText('AT RISK');
    expect(badges.length).toBe(1);
  });

  it('clicking a student expands detail panel', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    const button = screen.getByRole('button', { name: /Alice/ });
    fireEvent.click(button);
    const allCells = screen.getAllByRole('cell');
    const cellTexts = allCells.map((c) => c.textContent);
    expect(cellTexts).toContain('Wk 1');
    expect(cellTexts).toContain('Wk 2');
  });

  it('expanded detail groups absences by course with sub-headers', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    fireEvent.click(screen.getByRole('button', { name: /Alice/ }));
    const sectionHeadings = screen.getAllByRole('heading', { level: 4 });
    const headingTexts = sectionHeadings.map((h) => h.textContent);
    expect(headingTexts.some((t) => t.includes('SAT Math'))).toBe(true);
    expect(headingTexts.some((t) => t.includes('English Adv'))).toBe(true);
  });

  it('expanded detail shows date and session number for each absence', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    fireEvent.click(screen.getByRole('button', { name: /Alice/ }));
    expect(screen.getByText('2026-06-10')).toBeTruthy();
    expect(screen.getByText('2026-06-17')).toBeTruthy();
    expect(screen.getByText('2026-06-11')).toBeTruthy();
  });

  it('clicking the same student twice collapses the detail', () => {
    render(<AbsenceList students={[alice]} sessions={sessions} />);
    const button = screen.getByRole('button', { name: /Alice/ });
    fireEvent.click(button);
    expect(screen.getByText('2026-06-10')).toBeTruthy();
    fireEvent.click(button);
    expect(screen.queryByText('2026-06-10')).toBeNull();
  });

  it('only one student can be expanded at a time', () => {
    render(<AbsenceList students={[alice, carol]} sessions={sessions} />);
    fireEvent.click(screen.getByRole('button', { name: /Alice/ }));
    fireEvent.click(screen.getByRole('button', { name: /Carol/ }));
    const allCells = screen.getAllByRole('cell');
    const cellTexts = allCells.map((c) => c.textContent);
    expect(cellTexts).toContain('Wk 2');
    expect(cellTexts).toContain('2026-06-17');
    expect(cellTexts).not.toContain('2026-06-10');
    expect(cellTexts).not.toContain('2026-06-11');
  });

  it('shows empty message when students array is empty', () => {
    render(<AbsenceList students={[]} sessions={sessions} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });

  it('shows empty message when students is null', () => {
    render(<AbsenceList students={null} sessions={sessions} />);
    expect(screen.getByText(/No students/)).toBeTruthy();
  });
});
