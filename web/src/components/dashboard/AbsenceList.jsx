import React, { useMemo, useState } from 'react';

const buildStudentSummaries = (students, sessions) => {
  const sessionLookup = new Map();
  for (const s of sessions || []) {
    sessionLookup.set(s.sessionId, s);
  }

  const summaries = [];
  for (const student of students || []) {
    const absentSessions = (student.perSession || []).filter((ps) => ps.status === 'absent');
    if (absentSessions.length === 0) continue;

    const byCourse = new Map();
    for (const ps of absentSessions) {
      const sess = sessionLookup.get(ps.sessionId);
      if (!sess) continue;
      if (!byCourse.has(sess.courseId)) {
        byCourse.set(sess.courseId, {
          courseId: sess.courseId,
          courseName: sess.courseName,
          absences: [],
        });
      }
      byCourse.get(sess.courseId).absences.push(ps);
    }

    const courses = Array.from(byCourse.values())
      .map((c) => ({
        ...c,
        absences: [...c.absences].sort((a, b) =>
          (a.sessionDate || '').localeCompare(b.sessionDate || ''),
        ),
      }))
      .sort((a, b) => (a.courseName || '').localeCompare(b.courseName || ''));

    summaries.push({
      student,
      totalAbsences: absentSessions.length,
      courses,
    });
  }

  return summaries.sort((a, b) => {
    if (a.student.atRisk !== b.student.atRisk) return a.student.atRisk ? -1 : 1;
    if (b.totalAbsences !== a.totalAbsences) return b.totalAbsences - a.totalAbsences;
    return (a.student.name || '').localeCompare(b.student.name || '');
  });
};

function StudentSummary({ summary, expanded, onToggle }) {
  const { student, totalAbsences, courses } = summary;
  const courseNames = courses.map((c) => c.courseName).join(', ');

  return (
    <div
      style={{
        background: 'var(--color-bg, #FFFFFF)',
        border: '1px solid var(--color-border, #DCDBDD)',
        borderRadius: 'var(--radius-lg, 12px)',
        overflow: 'hidden',
        borderLeft: student.atRisk
          ? '3px solid var(--color-warning, #7A631C)'
          : '3px solid transparent',
      }}
    >
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        style={{
          width: '100%',
          padding: 'var(--space-4, 16px)',
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
          textAlign: 'left',
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--space-3, 12px)',
          font: 'inherit',
          color: 'inherit',
        }}
      >
        <div
          style={{
            width: '36px',
            height: '36px',
            borderRadius: '50%',
            background: student.atRisk
              ? 'color-mix(in srgb, var(--color-warning, #7A631C) 12%, transparent)'
              : 'var(--color-bg-hover, #F1F2F4)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: '14px',
            fontWeight: '600',
            color: student.atRisk ? 'var(--color-warning, #7A631C)' : 'var(--color-text-secondary, #4F5056)',
            flexShrink: 0,
          }}
        >
          {(student.name || '?')[0].toUpperCase()}
        </div>

        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--space-2, 8px)',
              flexWrap: 'wrap',
            }}
          >
            <span style={{ fontSize: '15px', fontWeight: '600', color: 'var(--color-text-primary, #111113)' }}>
              {student.studentId ? `${student.studentId} ` : ''}{student.name}
            </span>
            {student.atRisk && (
              <span
                style={{
                  fontSize: '10px',
                  fontWeight: '600',
                  padding: '2px 6px',
                  borderRadius: 'var(--radius-sm, 4px)',
                  background: 'var(--color-warning-bg, #FAF0C4)',
                  color: 'var(--color-warning, #7A631C)',
                  whiteSpace: 'nowrap',
                }}
              >
                AT RISK
              </span>
            )}
          </div>
          <div
            style={{
              fontSize: '12px',
              color: 'var(--color-text-muted, #696A6C)',
              marginTop: '2px',
            }}
          >
            {totalAbsences} absence{totalAbsences !== 1 ? 's' : ''} · {courseNames}
          </div>
        </div>

        <span
          aria-hidden="true"
          style={{
            fontSize: '14px',
            color: 'var(--color-text-muted, #696A6C)',
            transform: expanded ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.15s ease',
            flexShrink: 0,
          }}
        >
          ▼
        </span>
      </button>

      {expanded && <StudentDetail courses={courses} />}
    </div>
  );
}

function StudentDetail({ courses }) {
  return (
    <div
      style={{
        borderTop: '1px solid var(--color-border-subtle, #EEEFF1)',
        background: 'var(--color-bg-subtle, #F5F5F5)',
        padding: 'var(--space-3, 12px) var(--space-4, 16px) var(--space-4, 16px)',
      }}
    >
      {courses.map((course) => (
        <div
          key={course.courseId}
          style={{
            marginBottom: 'var(--space-3, 12px)',
            background: 'var(--color-bg, #FFFFFF)',
            border: '1px solid var(--color-border-subtle, #EEEFF1)',
            borderRadius: 'var(--radius-md, 8px)',
            overflow: 'hidden',
          }}
        >
          <h4
            style={{
              margin: 0,
              padding: '8px 12px',
              fontSize: '0.8125rem',
              fontWeight: '600',
              color: 'var(--color-text-primary, #111113)',
              background: 'var(--color-bg-subtle, #F5F5F5)',
              borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
            }}
          >
            {course.courseName}
            <span
              style={{
                marginLeft: '8px',
                fontSize: '0.6875rem',
                fontWeight: '500',
                color: 'var(--color-text-muted, #696A6C)',
              }}
            >
              {course.absences.length} absence{course.absences.length !== 1 ? 's' : ''}
            </span>
          </h4>

          <table
            style={{
              width: '100%',
              borderCollapse: 'collapse',
              fontSize: '0.8125rem',
            }}
          >
            <thead>
              <tr>
                <th
                  scope="col"
                  style={{
                    textAlign: 'left',
                    padding: '6px 12px',
                    fontSize: '0.6875rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                    width: '60px',
                  }}
                >
                  ครั้งที่
                </th>
                <th
                  scope="col"
                  style={{
                    textAlign: 'left',
                    padding: '6px 12px',
                    fontSize: '0.6875rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                    width: '120px',
                  }}
                >
                  Date
                </th>
                <th
                  scope="col"
                  style={{
                    textAlign: 'left',
                    padding: '6px 12px',
                    fontSize: '0.6875rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                  }}
                >
                  Session
                </th>
              </tr>
            </thead>
            <tbody>
              {course.absences.map((absence) => (
                <tr key={absence.sessionId}>
                  <td
                    style={{
                      padding: '8px 12px',
                      color: 'var(--color-text-primary, #111113)',
                      fontVariantNumeric: 'tabular-nums',
                    }}
                  >
                    {absence.sessionNumber}
                  </td>
                  <td
                    style={{
                      padding: '8px 12px',
                      color: 'var(--color-text-secondary, #4F5056)',
                    }}
                  >
                    {absence.sessionDate || '—'}
                  </td>
                  <td
                    style={{
                      padding: '8px 12px',
                      color: 'var(--color-text-primary, #111113)',
                    }}
                  >
                    {absence.sessionName || `Session ${absence.sessionNumber}`}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </div>
  );
}

export function AbsenceList({ students, sessions }) {
  const summaries = useMemo(() => buildStudentSummaries(students, sessions), [students, sessions]);
  const [expandedStudentId, setExpandedStudentId] = useState(null);

  if (!students || students.length === 0) {
    return (
      <div
        style={{
          textAlign: 'center',
          padding: 'var(--space-8, 32px)',
          color: 'var(--color-text-secondary, #4F5056)',
        }}
      >
        <p style={{ fontSize: '1rem', fontWeight: '500', marginBottom: '4px' }}>
          No students with absences
        </p>
        <p style={{ fontSize: '0.875rem', color: 'var(--color-text-muted, #696A6C)' }}>
          All attendance rates are above the threshold
        </p>
      </div>
    );
  }

  if (summaries.length === 0) {
    return (
      <div
        style={{
          textAlign: 'center',
          padding: 'var(--space-8, 32px)',
          color: 'var(--color-text-secondary, #4F5056)',
        }}
      >
        <p style={{ fontSize: '1rem', fontWeight: '500' }}>No absences in the current filter</p>
      </div>
    );
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--space-3, 12px)',
      }}
    >
      {summaries.map((summary) => (
        <StudentSummary
          key={summary.student.studentId || summary.student.name}
          summary={summary}
          expanded={expandedStudentId === (summary.student.studentId || summary.student.name)}
          onToggle={() => {
            const id = summary.student.studentId || summary.student.name;
            setExpandedStudentId((prev) => (prev === id ? null : id));
          }}
        />
      ))}
    </div>
  );
}
