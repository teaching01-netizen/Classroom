import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AttendanceRow } from '../components/AttendanceRow';

const baseSessions = [
  { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', status: 'done' },
  { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', status: 'active' },
  { sessionId: 's3', sessionNumber: 3, name: 'Wk 3', status: 'not_started' },
];

const baseStudent = {
  studentId: 'stu-1',
  name: 'Alice',
  nickname: '',
  avatarUrl: '',
  school: '',
  attendedSessions: 1,
  totalSessions: 3,
  attendanceRate: 0.33,
  atRisk: true,
  perSession: [
    { sessionId: 's1', sessionNumber: 1, sessionName: 'Wk 1', sessionStatus: 'done', checkedIn: true, status: 'ok' },
    { sessionId: 's2', sessionNumber: 2, sessionName: 'Wk 2', sessionStatus: 'active', checkedIn: false, status: 'ok' },
    { sessionId: 's3', sessionNumber: 3, sessionName: 'Wk 3', sessionStatus: 'not_started', checkedIn: false, status: 'ok' },
  ],
};

describe('AttendanceRow', () => {
  it('shows ✓ for done session where student checked in', () => {
    render(
      <table><tbody>
        <AttendanceRow student={baseStudent} sessions={baseSessions} />
      </tbody></table>
    );
    // First cell: done + checked in → ✓
    expect(screen.getByText('✓')).toBeTruthy();
  });

  it('shows ✗ for done session where student did not check in', () => {
    const student = {
      ...baseStudent,
      perSession: [
        { ...baseStudent.perSession[0], checkedIn: false },
        baseStudent.perSession[1],
        baseStudent.perSession[2],
      ],
    };
    render(
      <table><tbody>
        <AttendanceRow student={student} sessions={baseSessions} />
      </tbody></table>
    );
    expect(screen.getByText('✗')).toBeTruthy();
  });

  it('shows "—" for active session (not started yet)', () => {
    render(
      <table><tbody>
        <AttendanceRow student={baseStudent} sessions={baseSessions} />
      </tbody></table>
    );
    // Active and not_started sessions should show —
    const dashes = screen.getAllByText('—');
    expect(dashes.length).toBe(2);
  });

  it('shows "—" for not_started session', () => {
    render(
      <table><tbody>
        <AttendanceRow student={baseStudent} sessions={baseSessions} />
      </tbody></table>
    );
    const dashes = screen.getAllByText('—');
    expect(dashes.length).toBe(2);
  });

  it('shows ! for error status', () => {
    const student = {
      ...baseStudent,
      perSession: [
        { ...baseStudent.perSession[0], status: 'error' },
        baseStudent.perSession[1],
        baseStudent.perSession[2],
      ],
    };
    render(
      <table><tbody>
        <AttendanceRow student={student} sessions={baseSessions} />
      </tbody></table>
    );
    expect(screen.getByText('!')).toBeTruthy();
  });
});
