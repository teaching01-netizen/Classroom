import React, { useMemo } from 'react';

const buildSections = (students, sessions) => {
  const sessionLookup = new Map();
  for (const s of sessions || []) {
    sessionLookup.set(s.sessionId, s);
  }

  const sectionsByCourse = new Map();
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

    for (const course of byCourse.values()) {
      if (!sectionsByCourse.has(course.courseId)) {
        sectionsByCourse.set(course.courseId, {
          courseId: course.courseId,
          courseName: course.courseName,
          rows: [],
        });
      }
      const sorted = [...course.absences].sort((a, b) =>
        (a.sessionDate || '').localeCompare(b.sessionDate || ''),
      );
      const latest = sorted[sorted.length - 1];
      sectionsByCourse.get(course.courseId).rows.push({
        student,
        absenceCount: course.absences.length,
        latestDate: latest?.sessionDate || '',
        latestSessionName: latest?.sessionName || (latest ? `Session ${latest.sessionNumber}` : ''),
      });
    }
  }

  return Array.from(sectionsByCourse.values())
    .map((s) => ({
      ...s,
      rows: s.rows.sort((a, b) => {
        if (b.absenceCount !== a.absenceCount) return b.absenceCount - a.absenceCount;
        return (a.student.name || '').localeCompare(b.student.name || '');
      }),
    }))
    .sort((a, b) => (a.courseName || '').localeCompare(b.courseName || ''));
};

export function AbsenceList({ students, sessions }) {
  const sections = useMemo(() => buildSections(students, sessions), [students, sessions]);

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

  if (sections.length === 0) {
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
        gap: 'var(--space-6, 24px)',
      }}
    >
      {sections.map((section) => (
        <section
          key={section.courseId}
          style={{
            background: 'var(--color-bg, #FFFFFF)',
            border: '1px solid var(--color-border, #DCDBDD)',
            borderRadius: 'var(--radius-lg, 12px)',
            overflow: 'hidden',
          }}
        >
          <h3
            style={{
              padding: 'var(--space-3, 12px) var(--space-4, 16px)',
              fontSize: '0.9375rem',
              fontWeight: '600',
              color: 'var(--color-text-primary, #111113)',
              background: 'var(--color-bg-subtle, #F5F5F5)',
              borderBottom: '1px solid var(--color-border, #DCDBDD)',
              margin: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}
          >
            <span>{section.courseName}</span>
            <span
              style={{
                fontSize: '0.75rem',
                fontWeight: '500',
                color: 'var(--color-text-muted, #696A6C)',
              }}
            >
              {section.rows.length} student{section.rows.length !== 1 ? 's' : ''} absent
            </span>
          </h3>

          <table
            style={{
              width: '100%',
              borderCollapse: 'collapse',
              fontSize: '0.875rem',
            }}
          >
            <thead>
              <tr>
                <th
                  scope="col"
                  style={{
                    textAlign: 'left',
                    padding: '8px 16px',
                    fontSize: '0.75rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                  }}
                >
                  Student
                </th>
                <th
                  scope="col"
                  style={{
                    textAlign: 'right',
                    padding: '8px 16px',
                    fontSize: '0.75rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                    width: '100px',
                  }}
                >
                  Absences
                </th>
                <th
                  scope="col"
                  style={{
                    textAlign: 'left',
                    padding: '8px 16px',
                    fontSize: '0.75rem',
                    fontWeight: '600',
                    color: 'var(--color-text-muted, #696A6C)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
                  }}
                >
                  Latest
                </th>
              </tr>
            </thead>
            <tbody>
              {section.rows.map((row) => (
                <tr
                  key={row.student.studentId || row.student.name}
                  style={{
                    borderTop: '1px solid var(--color-border-subtle, #EEEFF1)',
                  }}
                >
                  <td
                    style={{
                      padding: '10px 16px',
                      color: 'var(--color-text-primary, #111113)',
                    }}
                  >
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--space-2, 8px)',
                      }}
                    >
                      <span style={{ fontWeight: '500' }}>{row.student.name}</span>
                      {row.student.atRisk && (
                        <span
                          style={{
                            fontSize: '0.625rem',
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
                  </td>
                  <td
                    style={{
                      padding: '10px 16px',
                      textAlign: 'right',
                      color: 'var(--color-text-primary, #111113)',
                      fontVariantNumeric: 'tabular-nums',
                    }}
                  >
                    {row.absenceCount}
                  </td>
                  <td
                    style={{
                      padding: '10px 16px',
                      color: 'var(--color-text-secondary, #4F5056)',
                      fontSize: '0.8125rem',
                    }}
                  >
                    {row.latestDate || '—'}
                    {row.latestSessionName && (
                      <span
                        style={{
                          marginLeft: '8px',
                          color: 'var(--color-text-muted, #696A6C)',
                        }}
                      >
                        · {row.latestSessionName}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ))}
    </div>
  );
}
