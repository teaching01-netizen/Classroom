import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { CheckinDetail } from '../pages/CheckinDetail';

const mockStudents = [
  { student_id: 'W260101', name: 'Siriwan Assumption', nickname: 'Unun', school: 'Assumption Convent', checked_in: true, participation_points: 10, avatar_url: '' },
  { student_id: 'W260024', name: 'Nattapong Pathumwan', nickname: 'Win', school: 'Pathumwan Demonstration School', checked_in: true, participation_points: 8, avatar_url: '' },
  { student_id: 'W260032', name: 'Kittipong Newton', nickname: 'KB', school: 'Newton Sixth Form', checked_in: true, participation_points: 12, avatar_url: '' },
  { student_id: 'W260039', name: 'Somchai Panyarat', nickname: 'Good', school: 'Panyarat', checked_in: true, participation_points: 5, avatar_url: '' },
  { student_id: 'W260055', name: 'Thammasak Andrews', nickname: 'I-Tim', school: 'St.Andrews ekkamai', checked_in: false, participation_points: 7, avatar_url: '' },
];

vi.mock('react-router-dom', () => ({
  useParams: () => ({ courseId: '1', sessionId: '1' }),
  Link: ({ children, ...props }) => <a {...props}>{children}</a>,
}));

vi.mock('../hooks/useCheckins', () => ({
  useCheckins: () => ({
    students: mockStudents,
    currentSession: { name: 'Session 1' },
    isLoading: false,
    isRefreshing: false,
    error: null,
    toggleCheckin: vi.fn(),
  }),
}));

vi.mock('../store/useSessionStore', () => ({
  useSessionStore: (selector) => {
    if (selector) return selector({ courseName: 'Test Course' });
    return { courseName: 'Test Course' };
  },
}));

describe('CheckinDetail search', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders all students initially', () => {
    render(<CheckinDetail />);
    expect(screen.getByText('Unun')).toBeTruthy();
    expect(screen.getByText('Win')).toBeTruthy();
    expect(screen.getByText('KB')).toBeTruthy();
    expect(screen.getByText('Good')).toBeTruthy();
    expect(screen.getByText('I-Tim')).toBeTruthy();
  });

  it('filters students by nickname', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'Win' } });

    expect(screen.getByText('Win')).toBeTruthy();
    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('KB')).toBeNull();
    expect(screen.queryByText('Good')).toBeNull();
    expect(screen.queryByText('I-Tim')).toBeNull();
  });

  it('filters students by partial nickname', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'I-Tim' } });

    expect(screen.getByText('I-Tim')).toBeTruthy();
    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('Win')).toBeNull();
    expect(screen.queryByText('KB')).toBeNull();
    expect(screen.queryByText('Good')).toBeNull();
  });

  it('filters students by full name', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'Nattapong' } });

    expect(screen.getByText('Win')).toBeTruthy();
    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('KB')).toBeNull();
    expect(screen.queryByText('Good')).toBeNull();
    expect(screen.queryByText('I-Tim')).toBeNull();
  });

  it('filters students by student_id (wcode)', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'W260032' } });

    expect(screen.getByText('KB')).toBeTruthy();
    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('Win')).toBeNull();
    expect(screen.queryByText('Good')).toBeNull();
    expect(screen.queryByText('I-Tim')).toBeNull();
  });

  it('search is case-insensitive', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'win' } });

    expect(screen.getByText('Win')).toBeTruthy();
    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('KB')).toBeNull();
  });

  it('shows all students when search is cleared', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');

    fireEvent.change(searchInput, { target: { value: 'Win' } });
    expect(screen.queryByText('Unun')).toBeNull();

    fireEvent.change(searchInput, { target: { value: '' } });
    expect(screen.getByText('Unun')).toBeTruthy();
    expect(screen.getByText('Win')).toBeTruthy();
    expect(screen.getByText('KB')).toBeTruthy();
    expect(screen.getByText('Good')).toBeTruthy();
    expect(screen.getByText('I-Tim')).toBeTruthy();
  });

  it('shows no students when search matches nothing', () => {
    render(<CheckinDetail />);
    const searchInput = screen.getByPlaceholderText('Search students...');
    fireEvent.change(searchInput, { target: { value: 'zzzzz' } });

    expect(screen.queryByText('Unun')).toBeNull();
    expect(screen.queryByText('Win')).toBeNull();
    expect(screen.queryByText('KB')).toBeNull();
    expect(screen.queryByText('Good')).toBeNull();
    expect(screen.queryByText('I-Tim')).toBeNull();
    expect(screen.getByText('No students match your search')).toBeTruthy();
  });
});
