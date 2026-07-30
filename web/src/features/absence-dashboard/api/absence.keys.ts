import type { DashboardFilters } from './absence.schemas'

export const absenceKeys = {
  all: ['absence-dashboard'] as const,
  report: (filters: DashboardFilters) => [...absenceKeys.all, 'report', filters] as const,
  views: () => [...absenceKeys.all, 'views'] as const,
} as const
