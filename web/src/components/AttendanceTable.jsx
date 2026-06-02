import React from 'react';
import { AttendanceRow } from './AttendanceRow';

export function AttendanceTable({ report }) {
  if (!report || !report.sessions || report.sessions.length === 0) {
    return (
      <div style={{ textAlign: 'center', padding: '32px', color: 'var(--color-text-secondary, #4F5056)' }}>
        No session data available.
      </div>
    );
  }

  const { sessions, students } = report;

  return (
    <div style={{ overflowX: 'auto', borderRadius: 'var(--radius-md, 8px)', border: '1px solid var(--color-border, #DCDBDD)' }}>
      <table
        style={{
          width: '100%',
          borderCollapse: 'collapse',
          fontSize: '13px',
          fontFamily: 'var(--font-sans)',
        }}
      >
        <thead>
          <tr style={{ background: 'var(--color-bg-subtle, #F5F5F5)' }}>
            <th
              style={{
                position: 'sticky',
                left: 0,
                background: 'var(--color-bg-subtle, #F5F5F5)',
                zIndex: 2,
                padding: '10px 12px',
                textAlign: 'left',
                fontWeight: '600',
                fontSize: '12px',
                color: 'var(--color-text-secondary, #4F5056)',
                borderBottom: '1px solid var(--color-border, #DCDBDD)',
                minWidth: '180px',
              }}
            >
              Student
            </th>

            {sessions.map((sess, idx) => (
              <th
                key={sess.sessionId || idx}
                style={{
                  padding: '10px 8px',
                  textAlign: 'center',
                  fontWeight: '600',
                  fontSize: '11px',
                  color: 'var(--color-text-secondary, #4F5056)',
                  borderBottom: '1px solid var(--color-border, #DCDBDD)',
                  minWidth: '48px',
                  whiteSpace: 'nowrap',
                }}
              >
                <div>{sess.sessionNumber || idx + 1}</div>
                {sess.name && (
                  <div
                    style={{
                      fontSize: '10px',
                      fontWeight: '400',
                      color: 'var(--color-text-muted, #696A6C)',
                      maxWidth: '80px',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                    title={sess.name}
                  >
                    {sess.name}
                  </div>
                )}
              </th>
            ))}

            <th
              style={{
                padding: '10px 8px',
                textAlign: 'center',
                fontWeight: '600',
                fontSize: '11px',
                color: 'var(--color-text-secondary, #4F5056)',
                borderBottom: '1px solid var(--color-border, #DCDBDD)',
                whiteSpace: 'nowrap',
              }}
            >
              Sessions
            </th>

            <th
              style={{
                padding: '10px 8px',
                textAlign: 'center',
                fontWeight: '600',
                fontSize: '11px',
                color: 'var(--color-text-secondary, #4F5056)',
                borderBottom: '1px solid var(--color-border, #DCDBDD)',
                whiteSpace: 'nowrap',
              }}
            >
              Rate
            </th>
          </tr>
        </thead>
        <tbody>
          {students.map((student) => (
            <AttendanceRow key={student.studentId} student={student} sessions={sessions} />
          ))}
        </tbody>
      </table>

      {students.length === 0 && (
        <div style={{ textAlign: 'center', padding: '32px', color: 'var(--color-text-secondary, #4F5056)' }}>
          No student attendance data found.
        </div>
      )}
    </div>
  );
}
