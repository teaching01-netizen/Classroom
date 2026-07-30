import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import {
  absenceDashboardSchema,
  dashboardViewSchema,
  dashboardViewsSchema,
  type DashboardFilters,
} from './absence.schemas'
import { absenceKeys } from './absence.keys'

export function useAbsenceDashboardQuery(
  filters: DashboardFilters,
  enabled: boolean,
) {
  return useQuery({
    queryKey: absenceKeys.report(filters),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.absenceDashboard(JSON.stringify(filters)), {
        schema: absenceDashboardSchema,
        signal,
        timeoutMs: 65_000,
      }),
    enabled,
  })
}

export function useDashboardViewsQuery() {
  return useQuery({
    queryKey: absenceKeys.views(),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.dashboardViews, {
        schema: dashboardViewsSchema,
        signal,
      }),
  })
}

type SaveViewVariables = {
  readonly id?: number
  readonly name: string
  readonly filters: DashboardFilters
}

export function useSaveDashboardViewMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, name, filters }: SaveViewVariables) =>
      id === undefined
        ? apiClient.post(endpoints.dashboardViews, {
            body: { name, filters },
            schema: dashboardViewSchema,
          })
        : apiClient.put(endpoints.dashboardView(id), {
            body: { name, filters },
            schema: dashboardViewSchema,
          }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: absenceKeys.views() }),
  })
}

export function useDeleteDashboardViewMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      apiClient.delete(endpoints.dashboardView(id), { schema: z.unknown() }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: absenceKeys.views() }),
  })
}

export function useTouchDashboardViewMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) =>
      apiClient.post(endpoints.touchDashboardView(id), {
        schema: z.unknown(),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: absenceKeys.views() }),
  })
}
