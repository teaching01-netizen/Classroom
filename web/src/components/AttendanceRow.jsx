import React from 'react';

export function AttendanceRow({ student, sessions }) {
  const ratePercent = Math.round(student.attendanceRate * 100);

  return (
    <tr
      style={{
        background: student.atRisk
          ? 'color-mix(in srgb, var(--color-warning, #7A631C) 6%, transparent)'
          : 'transparent',
        borderLeft: student.atRisk
          ? '3px solid var(--color-warning, #7A631C)'
          : '3px solid transparent',
      }}
    >
      <td
        style={{
          position: 'sticky',
          left: 0,
          background: student.atRisk
            ? 'color-mix(in srgb, var(--color-warning, #7A631C) 6%, var(--color-bg, #FFFFFF))'
            : 'var(--color-bg, #FFFFFF)',
          zIndex: 1,
          padding: '10px 12px',
          borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
          minWidth: '180px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          {student.avatarUrl ? (
            <img
              src={student.avatarUrl}
              alt=""
              style={{ width: '28px', height: '28px', borderRadius: '50%', objectFit: 'cover' }}
            />
          ) : (
            <div
              style={{
                width: '28px',
                height: '28px',
                borderRadius: '50%',
                background: 'var(--color-bg-hover, #F1F2F4)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: '12px',
                fontWeight: '600',
                color: 'var(--color-text-secondary, #4F5056)',
                flexShrink: 0,
              }}
            >
              {(student.name || '?')[0].toUpperCase()}
            </div>
          )}
          <div>
            <div style={{ fontSize: '14px', fontWeight: '500', color: 'var(--color-text-primary, #111113)' }}>
              {student.name}
              {student.nickname && student.nickname !== student.name && (
                <span style={{ fontWeight: '400', color: 'var(--color-text-muted, #696A6C)', marginLeft: '4px' }}>
                  ({student.nickname})
                </span>
              )}
            </div>
            {student.school && (
              <div style={{ fontSize: '11px', color: 'var(--color-text-muted, #696A6C)' }}>
                {student.school}
              </div>
            )}
          </div>
          {student.atRisk && (
            <span
              style={{
                marginLeft: 'auto',
                fontSize: '10px',
                fontWeight: '600',
                padding: '2px 6px',
                borderRadius: 'var(--radius-sm, 4px)',
                background: 'var(--color-warning-bg, #FAF0C4)',
                color: 'var(--color-warning, #7A631C)',
                whiteSpace: 'nowrap',
                flexShrink: 0,
              }}
            >
              AT RISK
            </span>
          )}
        </div>
      </td>

      {sessions.map((sess, idx) => {
        const cell = student.perSession?.[idx];
        let symbol = '—';
        let cellColor = 'var(--color-text-disabled, #B8BCC4)';

        if (cell?.status === 'ok') {
          if (cell.checkedIn) {
            symbol = '✓';
            cellColor = 'var(--color-success, #257348)';
          } else {
            symbol = '✗';
            cellColor = 'var(--color-danger, #9A3D4A)';
          }
        } else if (cell?.status === 'error') {
          symbol = '!';
          cellColor = 'var(--color-warning, #7A631C)';
        }

        return (
          <td
            key={sess.sessionId || idx}
            style={{
              textAlign: 'center',
              padding: '10px 8px',
              borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
              fontSize: '14px',
              fontWeight: cell?.status === 'ok' ? '600' : '400',
              color: cellColor,
              minWidth: '48px',
            }}
          >
            {symbol}
          </td>
        );
      })}

      <td
        style={{
          textAlign: 'center',
          padding: '10px 8px',
          borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
          fontSize: '13px',
          color: 'var(--color-text-secondary, #4F5056)',
          whiteSpace: 'nowrap',
        }}
      >
        {student.attendedSessions}/{student.totalSessions}
      </td>

      <td
        style={{
          textAlign: 'center',
          padding: '10px 8px',
          borderBottom: '1px solid var(--color-border-subtle, #EEEFF1)',
          fontSize: '13px',
          fontWeight: '600',
          color:
            ratePercent >= 80
              ? 'var(--color-success, #257348)'
              : ratePercent >= 50
              ? 'var(--color-warning, #7A631C)'
              : 'var(--color-danger, #9A3D4A)',
          whiteSpace: 'nowrap',
        }}
      >
        {ratePercent}%
      </td>
    </tr>
  );
}
