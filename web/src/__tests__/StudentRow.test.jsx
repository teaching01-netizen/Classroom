import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { StudentRow } from '../components/StudentRow';

const baseStudent = {
  student_id: 'stu-1',
  name: 'Alice Smith',
  nickname: 'Ali',
  school: 'Concord',
  avatar_url: '',
  checked_in: true,
  participation_points: 5,
};

describe('StudentRow', () => {
  it('shows nickname instead of full name when nickname is provided', () => {
    render(
      <table><tbody>
        <StudentRow student={baseStudent} onToggleCheckin={vi.fn()} index={0} />
      </tbody></table>
    );
    expect(screen.getByText('Ali')).toBeTruthy();
    expect(screen.queryByText('Alice Smith')).toBeNull();
  });

  it('falls back to full name when nickname is empty', () => {
    const student = { ...baseStudent, nickname: '' };
    render(
      <table><tbody>
        <StudentRow student={student} onToggleCheckin={vi.fn()} index={0} />
      </tbody></table>
    );
    expect(screen.getByText('Alice Smith')).toBeTruthy();
  });

  it('falls back to full name when nickname is missing', () => {
    const student = { ...baseStudent, nickname: undefined };
    render(
      <table><tbody>
        <StudentRow student={student} onToggleCheckin={vi.fn()} index={0} />
      </tbody></table>
    );
    expect(screen.getByText('Alice Smith')).toBeTruthy();
  });

  it('shows student_id (wcode) below the name', () => {
    render(
      <table><tbody>
        <StudentRow student={baseStudent} onToggleCheckin={vi.fn()} index={0} />
      </tbody></table>
    );
    expect(screen.getByText('stu-1')).toBeTruthy();
  });

  it('shows school below the name', () => {
    render(
      <table><tbody>
        <StudentRow student={baseStudent} onToggleCheckin={vi.fn()} index={0} />
      </tbody></table>
    );
    expect(screen.getByText('Concord')).toBeTruthy();
  });
});
