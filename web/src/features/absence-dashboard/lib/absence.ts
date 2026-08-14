import { attendancePercent } from '@/features/attendance'

export function absenceRate(absences: number, totalSessions: number): number {
  if (totalSessions <= 0) {
    return 0
  }
  return attendancePercent(absences / totalSessions)
}
