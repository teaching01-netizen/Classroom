import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { courseIdSchema } from './course.schemas'
import { courseKeys } from './course.keys'
import { useToggleFavouriteMutation } from './course.mutations'

function FavouriteHarness({ queryClient }: { readonly queryClient: QueryClient }) {
  const mutation = useToggleFavouriteMutation()
  const courseId = courseIdSchema.parse('CS101')
  const pinned = queryClient.getQueryData<{ readonly favourite_ids: readonly string[] }>(
    courseKeys.favourites(),
  )?.favourite_ids.includes(courseId) ?? false
  return (
    <button
      type="button"
      onClick={() => mutation.mutate({ courseId, pinned: true })}
    >
      {pinned ? 'Pinned' : 'Not pinned'}
    </button>
  )
}

describe('favourite mutation', () => {
  it('rolls back its optimistic cache update after server rejection', async () => {
    // Given
    let rejectRequest: ((reason?: unknown) => void) | undefined
    const pendingRequest = new Promise<Response>((_resolve, reject) => {
      rejectRequest = reject
    })
    vi.spyOn(globalThis, 'fetch').mockReturnValue(pendingRequest)
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    queryClient.setQueryData(courseKeys.favourites(), { favourite_ids: [] })
    render(
      <QueryClientProvider client={queryClient}>
        <FavouriteHarness queryClient={queryClient} />
      </QueryClientProvider>,
    )
    // When
    fireEvent.click(screen.getByRole('button', { name: 'Not pinned' }))
    await waitFor(() => {
      expect(queryClient.getQueryData(courseKeys.favourites())).toEqual({
        favourite_ids: ['CS101'],
      })
    })
    rejectRequest?.(new Error('Network failed'))
    // Then
    await waitFor(() => {
      expect(queryClient.getQueryData(courseKeys.favourites())).toEqual({
        favourite_ids: [],
      })
    })
  })
})
