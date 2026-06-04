import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BrowserRouter } from 'react-router-dom';
import { DashboardCourseCard } from '../components/dashboard/DashboardCourseCard';

vi.mock('../store/usePinnedCoursesStore', () => ({
  usePinnedCoursesStore: vi.fn((selector) => {
    const state = { unpinCourse: vi.fn() };
    return selector(state);
  }),
}));

import { usePinnedCoursesStore } from '../store/usePinnedCoursesStore';

const mockCourse = {
  course_id: 'CS101',
  name: 'Computer Science 101',
  status: 'active',
  term_dates: 'Jan - Mar 2026',
  enrolled_count: 45,
  total_sessions: 12,
  avg_attendance_rate: 0.82,
};

const mockAttendanceData = {
  courseId: 'CS101',
  courseName: 'Computer Science 101',
  sessions: [
    { sessionId: 's1', sessionNumber: 1, name: 'Wk 1', status: 'done' },
    { sessionId: 's2', sessionNumber: 2, name: 'Wk 2', status: 'done' },
    { sessionId: 's3', sessionNumber: 3, name: 'Wk 3', status: 'done' },
    { sessionId: 's4', sessionNumber: 4, name: 'Wk 4', status: 'done' },
    { sessionId: 's5', sessionNumber: 5, name: 'Wk 5', status: 'done' },
  ],
  students: [
    {
      studentId: 'stu-1',
      name: 'Alice Wang',
      nickname: '',
      avatarUrl: '',
      school: '',
      attendedSessions: 2,
      totalSessions: 5,
      attendanceRate: 0.4,
      atRisk: true,
      perSession: [],
    },
    {
      studentId: 'stu-2',
      name: 'Bob Smith',
      nickname: '',
      avatarUrl: '',
      school: '',
      attendedSessions: 3,
      totalSessions: 5,
      attendanceRate: 0.6,
      atRisk: true,
      perSession: [],
    },
    {
      studentId: 'stu-3',
      name: 'Carol Davis',
      nickname: '',
      avatarUrl: '',
      school: '',
      attendedSessions: 5,
      totalSessions: 5,
      attendanceRate: 1.0,
      atRisk: false,
      perSession: [],
    },
  ],
  errors: [],
  truncated: false,
  threshold: 1,
  computedAt: '2026-06-01T00:00:00Z',
  durationMs: 1200,
};

function renderWithRouter(ui) {
  return render(<BrowserRouter>{ui}</BrowserRouter>);
}

describe('DashboardCourseCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    usePinnedCoursesStore.mockReturnValue({ unpinCourse: vi.fn() });
  });

  it('shows at-risk student names when attendanceData is provided', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.getByText('Alice Wang')).toBeTruthy();
    expect(screen.getByText('Bob Smith')).toBeTruthy();
  });

  it('does not show non-at-risk students in the at-risk list', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.queryByText('Carol Davis')).toBeNull();
  });

  it('shows at-risk count in the alert banner', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.getByText(/2 students at risk/)).toBeTruthy();
  });

  it('shows attendance rate percentage for each at-risk student', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.getByText('40%')).toBeTruthy();
    expect(screen.getByText('60%')).toBeTruthy();
  });

  it('shows absence count for each at-risk student', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.getByText('3/5 absences')).toBeTruthy();
    expect(screen.getByText('2/5 absences')).toBeTruthy();
  });

  it('shows course info when attendanceData is null (loading)', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={null} attendanceLoading={true} />
    );

    expect(screen.getByText('Computer Science 101')).toBeTruthy();
    expect(screen.getByText('CS101')).toBeTruthy();
    expect(screen.getByText('Loading attendance data...')).toBeTruthy();
  });

  it('shows success message when no students are at risk', () => {
    const safeData = {
      ...mockAttendanceData,
      students: mockAttendanceData.students.map((s) => ({ ...s, atRisk: false })),
    };

    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={safeData} />
    );

    expect(screen.getByText(/No students at risk/)).toBeTruthy();
  });

  it('shows "No completed sessions" when there are no done sessions', () => {
    const emptyData = {
      ...mockAttendanceData,
      sessions: [],
      students: [],
    };

    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={emptyData} />
    );

    expect(screen.getByText(/No completed sessions/)).toBeTruthy();
  });

  it('does not show attendance matrix by default', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    expect(screen.queryByText('Expand')).toBeTruthy();
    expect(screen.queryByText('Student')).toBeNull();
  });

  it('shows attendance matrix after clicking expand', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    fireEvent.click(screen.getByText('Expand'));

    expect(screen.queryByText('Expand')).toBeNull();
    expect(screen.queryByText('Collapse')).toBeTruthy();
  });

  it('hides attendance matrix after clicking collapse', () => {
    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={mockAttendanceData} />
    );

    fireEvent.click(screen.getByText('Expand'));
    expect(screen.queryByText('Collapse')).toBeTruthy();

    fireEvent.click(screen.getByText('Collapse'));
    expect(screen.queryByText('Expand')).toBeTruthy();
    expect(screen.queryByText('Collapse')).toBeNull();
  });

  it('does not show expand button when there are no done sessions', () => {
    const emptyData = {
      ...mockAttendanceData,
      sessions: [],
      students: [],
    };

    renderWithRouter(
      <DashboardCourseCard course={mockCourse} attendanceData={emptyData} />
    );

    expect(screen.queryByText('Expand')).toBeNull();
  });

  it('shows error message when attendanceError is set', () => {
    renderWithRouter(
      <DashboardCourseCard
        course={mockCourse}
        attendanceData={null}
        attendanceError="Server error"
      />
    );

    expect(screen.getByText('Attendance data unavailable')).toBeTruthy();
  });
});
