import { useMutation, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { courseKeys } from './course.keys'
import type { CourseId } from './course.schemas'

type Favourites = {
  readonly favourite_ids: readonly string[]
}

type ToggleFavourite = {
  readonly courseId: CourseId
  readonly pinned: boolean
}

export function useToggleFavouriteMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ courseId, pinned }: ToggleFavourite) =>
      pinned
        ? apiClient.post(endpoints.favourites, {
            body: { course_id: courseId },
            schema: z.unknown(),
          })
        : apiClient.delete(endpoints.favourite(courseId), {
            schema: z.unknown(),
          }),
    onMutate: async ({ courseId, pinned }) => {
      await queryClient.cancelQueries({ queryKey: courseKeys.favourites() })
      const previous = queryClient.getQueryData<Favourites>(courseKeys.favourites())
      queryClient.setQueryData<Favourites>(courseKeys.favourites(), (current) => {
        const ids = current?.favourite_ids ?? []
        return {
          favourite_ids: pinned
            ? Array.from(new Set([...ids, courseId]))
            : ids.filter((id) => id !== courseId),
        }
      })
      return { previous }
    },
    onError: (_error, _variables, context) => {
      queryClient.setQueryData(courseKeys.favourites(), context?.previous)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: courseKeys.favourites() }),
  })
}
