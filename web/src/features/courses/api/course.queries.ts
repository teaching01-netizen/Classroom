import { useQuery } from '@tanstack/react-query'
import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import { courseKeys } from './course.keys'
import { courseListSchema } from './course.schemas'

const favouritesSchema = z.object({
  favourite_ids: z.array(z.string()),
})

export function useCoursesQuery() {
  const connected = useConnectionStore((state) => state.status === 'connected')
  return useQuery({
    queryKey: courseKeys.all,
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.courses, { schema: courseListSchema, signal }),
    refetchInterval: connected ? false : 10_000,
  })
}

export function useFavouritesQuery() {
  return useQuery({
    queryKey: courseKeys.favourites(),
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.favourites, { schema: favouritesSchema, signal }),
  })
}
