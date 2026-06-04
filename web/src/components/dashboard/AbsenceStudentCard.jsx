import React, { useMemo } from 'react';

const sessionMapByCourse = (sessions) => {
  const map = {};
  for (const s of sessions) {
    map[s.sessionId] = s;
  }
  return map;
};

export function AbsenceStudentCard({ student, sessions }) {
  const sessionLookup = useMemo(() => sessionMapByCourse(sessions), [sessions]);

  const absences = useMemo(() => {
    return (student.perSession || []).filter((ps) => ps.status === 'absent');
  }, [student.perSession]);

  const groupedByCourse = useMemo(() => {
    const groups = {};
    for (const a of absences) {
      const sess = sessionLookup[a.sessionId];
      const courseName = sess?.courseName || 'Unknown';
      if (!groups[courseName]) {
        groups[courseName] = [];
      }
      groups[courseName].push(a);
    }
    return groups;
  }, [absences, sessionLookup]);

  const courseMap = useMemo(() => {
    const map = {};
    for (const c of student.courses || []) {
      map[c.courseName] = c;
    }
    return map;
  }, [student.courses]);

  const ratePercent = Math.round((student.attendanceRate || 0) * 100);
  const totalAbsences = student.totalSessions - student.attendedSessions;

  return (
    <div style={{
      background: 'var(--color-bg, #FFFFFF)',
      border: '1px solid var(--color-border, #DCDBDD)',
      borderRadius: 'var(--radius-lg, 12px)',
      overflow: 'hidden',
      borderLeft: student.atRisk
        ? '3px solid var(--color-warning, #7A631C)'
        : '3px solid transparent',
    }}>
      {/* Header */}
      <div style={{
        padding: 'var(--space-4, 16px) var(--space-4, 16px) var(--space-2, 8px)',
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-3, 12px)',
      }}>
        {student.avatarUrl ? (
          <img src={student.avatarUrl} alt="" style={{ width: '36px', height: '36px', borderRadius: '50%', objectFit: 'cover' }} />
        ) : (
          <div style={{
            width: '36px', height: '36px', borderRadius: '50%',
            background: student.atRisk
              ? 'color-mix(in srgb, var(--color-warning, #7A631C) 12%, transparent)'
              : 'var(--color-bg-hover, #F1F2F4)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: '14px', fontWeight: '600',
            color: student.atRisk ? 'var(--color-warning, #7A631C)' : 'var(--color-text-secondary, #4F5056)',
            flexShrink: 0,
          }}>
            {(student.name || '?')[0].toUpperCase()}
          </div>
        )}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2, 8px)', flexWrap: 'wrap' }}>
            <span style={{ fontSize: '15px', fontWeight: '600', color: 'var(--color-text-primary, #111113)' }}>
              {student.name}
              {student.nickname && student.nickname !== student.name && (
                <span style={{ fontWeight: '400', color: 'var(--color-text-muted, #696A6C)', marginLeft: '4px', fontSize: '13px' }}>
                  ({student.nickname})
                </span>
              )}
            </span>
            {student.atRisk && (
              <span style={{
                fontSize: '10px', fontWeight: '600',
                padding: '2px 6px', borderRadius: 'var(--radius-sm, 4px)',
                background: 'var(--color-warning-bg, #FAF0C4)',
                color: 'var(--color-warning, #7A631C)',
                whiteSpace: 'nowrap',
              }}>
                AT RISK
              </span>
            )}
          </div>
          {student.school && (
            <div style={{ fontSize: '12px', color: 'var(--color-text-muted, #696A6C)', marginTop: '1px' }}>
              {student.school}
            </div>
          )}
        </div>
        <div style={{ textAlign: 'right', flexShrink: 0 }}>
          <div style={{
            fontSize: '13px', fontWeight: '700',
            color: ratePercent >= 80 ? 'var(--color-success, #257348)' : 'var(--color-warning, #7A631C)',
          }}>
            {ratePercent}%
          </div>
          <div style={{ fontSize: '11px', color: 'var(--color-text-muted, #696A6C)' }}>
            {student.attendedSessions}/{student.totalSessions}
          </div>
        </div>
      </div>

      {/* Absence groups */}
      <div style={{ padding: '0 var(--space-4, 16px) var(--space-3, 12px)' }}>
        {Object.keys(groupedByCourse).length === 0 ? (
          <div style={{
            padding: 'var(--space-3, 12px)',
            fontSize: '13px', color: 'var(--color-text-muted, #696A6C)',
            textAlign: 'center',
          }}>
            No absences
          </div>
        ) : (
          Object.entries(groupedByCourse).map(([courseName, absencesList]) => {
            const course = courseMap[courseName];
            const absenceCount = absencesList.length;
            return (
              <div key={courseName} style={{
                marginBottom: 'var(--space-2, 8px)',
                borderRadius: 'var(--radius-md, 8px)',
                border: '1px solid var(--color-border-subtle, #EEEFF1)',
                overflow: 'hidden',
              }}>
                <div style={{
                  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                  padding: '8px 10px',
                  background: 'var(--color-bg-subtle, #F5F5F5)',
                  fontSize: '12px', fontWeight: '600',
                  color: course?.atRisk ? 'var(--color-warning, #7A631C)' : 'var(--color-text-secondary, #4F5056)',
                }}>
                  <span>{courseName}</span>
                  <span style={{ fontSize: '11px', fontWeight: '500', color: 'var(--color-text-muted, #696A6C)' }}>
                    {absenceCount} absence{absenceCount !== 1 ? 's' : ''}
                  </span>
                </div>
                {absencesList.map((absence) => (
                  <div key={absence.sessionId} style={{
                    display: 'flex', alignItems: 'center', gap: 'var(--space-2, 8px)',
                    padding: '7px 10px',
                    borderTop: '1px solid var(--color-border-subtle, #EEEFF1)',
                    fontSize: '13px',
                  }}>
                    <span style={{
                      width: '6px', height: '6px', borderRadius: '50%',
                      background: 'var(--color-danger, #9A3D4A)',
                      flexShrink: 0,
                    }} />
                    <span style={{ color: 'var(--color-text-muted, #696A6C)', fontSize: '12px', whiteSpace: 'nowrap' }}>
                      {absence.sessionDate || '—'}
                    </span>
                    <span style={{ color: 'var(--color-text-primary, #111113)' }}>
                      {absence.sessionName || `Session ${absence.sessionNumber}`}
                    </span>
                  </div>
                ))}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
