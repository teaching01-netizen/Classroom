import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/features/sessions'
import { useGivePointsMutation } from './points.queries'

vi.mock('@/shared/api/api-client', () => ({
  apiClient: {
    post: vi.fn(async () => ({ pointsAwarded: 2, participationPoints: 7 })),
  },
}))

describe('useGivePointsMutation', () => {
  it('posts the award and invalidates the session detail', async () => {
    const { apiClient } = await import('@/shared/api/api-client')
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )

    const { result } = renderHook(
      () => useGivePointsMutation('2317' as never, '20518' as never),
      { wrapper },
    )

    act(() => {
      result.current.mutate({ studentId: 'W123' as never, points: 2, expectedPoints: 5 })
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(apiClient.post).toHaveBeenCalledWith(
      '/api/teacher/courses/2317/sessions/20518/students/W123/points',
      expect.objectContaining({ body: { points: 2, expectedPoints: 5 } }),
    )
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: sessionKeys.detail('2317' as never, '20518' as never),
    })
  })
})
