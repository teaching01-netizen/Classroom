export function attendancePercent(rate: number): number {
  return Math.min(100, Math.max(0, Math.round(rate * 100)))
}

export function isAtRisk(rate: number, thresholdPercent: number): boolean {
  return attendancePercent(rate) < thresholdPercent
}
