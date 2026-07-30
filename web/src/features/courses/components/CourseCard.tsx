import { Link } from 'react-router-dom'
import { attendancePercent, type AttendanceReport } from '@/features/attendance'
import type { CourseSummary } from '../api/course.schemas'
import { Badge, type BadgeTone } from '@/shared/ui/Badge'
import { Button } from '@/shared/ui/Button'

const statusTone: Readonly<Record<CourseSummary['status'], BadgeTone>> = {
  active: 'success',
  upcoming: 'info',
  finished: 'neutral',
}

type CourseCardProps = {
  readonly course: CourseSummary
  readonly pinned: boolean
  readonly attendance?: AttendanceReport | undefined
  readonly attendancePending?: boolean
  readonly onTogglePinned: (pinned: boolean) => void
}

export function CourseCard({
  course,
  pinned,
  attendance,
  attendancePending = false,
  onTogglePinned,
}: CourseCardProps) {
  const percent = attendancePercent(course.avg_attendance_rate)
  const atRiskCount = attendance?.students.filter((student) => student.atRisk).length

  return (
    <article className="course-card">
      <div className="course-card__header">
        <Badge tone={statusTone[course.status]}>{course.status}</Badge>
        <Button
          aria-pressed={pinned}
          size="sm"
          variant="ghost"
          onClick={() => onTogglePinned(!pinned)}
        >
          {pinned ? 'Unpin' : 'Pin'}
        </Button>
      </div>
      <Link className="course-card__course-link" to={`/courses/${course.course_id}/sessions`}>
        <h3>{course.name}</h3>
        <p>{course.start_date} – {course.end_date}</p>
      </Link>
      <dl className="course-card__facts">
        <div><dt>Students</dt><dd>{course.enrolled_count}</dd></div>
        <div><dt>Sessions</dt><dd>{course.completed_sessions}/{course.total_sessions}</dd></div>
        <div><dt>Attendance</dt><dd>{percent}%</dd></div>
      </dl>
      <div
        aria-label={`${percent}% average attendance`}
        className="course-card__progress"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <span style={{ inlineSize: `${percent}%` }} />
      </div>
      {attendancePending && <p className="course-card__meta">Loading attendance detail…</p>}
      {atRiskCount !== undefined && (
        <p className="course-card__meta">
          {atRiskCount === 0 ? 'No students currently at risk' : `${atRiskCount} students at risk`}
        </p>
      )}
      <div className="course-card__actions">
        <span className="course-card__open-sessions" aria-hidden="true">Open sessions</span>
        <Link to={`/courses/${course.course_id}/attendance`}>Attendance report</Link>
      </div>
    </article>
  )
}
