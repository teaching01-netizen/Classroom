import React from 'react';
import { AbsenceStudentCard } from './AbsenceStudentCard';

export function AbsenceList({ students, sessions }) {
  if (!students || students.length === 0) {
    return (
      <div style={{
        textAlign: 'center',
        padding: 'var(--space-8, 32px)',
        color: 'var(--color-text-secondary, #4F5056)',
      }}>
        <p style={{ fontSize: '1rem', fontWeight: '500', marginBottom: '4px' }}>
          No students with absences
        </p>
        <p style={{ fontSize: '0.875rem', color: 'var(--color-text-muted, #696A6C)' }}>
          All attendance rates are above the threshold
        </p>
      </div>
    );
  }

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      gap: 'var(--space-3, 12px)',
    }}>
      {students.map((student) => (
        <AbsenceStudentCard
          key={student.studentId || student.name}
          student={student}
          sessions={sessions}
        />
      ))}
    </div>
  );
}
