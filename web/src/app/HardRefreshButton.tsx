import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { endpoints } from '@/shared/api/endpoints'
import { Button } from '@/shared/ui/Button'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'

const refreshAllResultSchema = z.object({
  courses_discovered: z.number(),
  courses_refreshed: z.number(),
  sessions_discovered: z.number(),
  sessions_refreshed: z.number(),
  profiles_refreshed: z.boolean(),
  failed_targets: z.number(),
})

function RefreshIcon() {
  return (
    <svg aria-hidden="true" fill="none" height="16" viewBox="0 0 16 16" width="16">
      <path d="M13.25 5.5A5.5 5.5 0 1 0 14 8" stroke="currentColor" strokeLinecap="round" strokeWidth="1.5" />
      <path d="M10.5 2.75h3v3" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.5" />
    </svg>
  )
}

type SyncFeedback = 'idle' | 'success' | 'error'

export function HardRefreshButton() {
  const queryClient = useQueryClient()
  const { announce } = useToast()
  const [feedback, setFeedback] = useState<SyncFeedback>('idle')
  const refresh = useMutation({
    mutationFn: () => apiClient.post(endpoints.refreshAllData, {
      schema: refreshAllResultSchema,
      timeoutMs: 120_000,
    }),
    onMutate: () => setFeedback('idle'),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ refetchType: 'all' })
      if (result.failed_targets === 0) {
        setFeedback('success')
        return
      }
      announce(`Data synced with ${result.failed_targets} item${result.failed_targets === 1 ? '' : 's'} still pending.`, 'error')
    },
    onError: (error) => {
      setFeedback('error')
      announce(getErrorMessage(error), 'error')
    },
  })
  const feedbackMessage = feedback === 'success'
    ? 'All data synced, including newly added courses.'
    : feedback === 'error'
      ? 'Data sync failed.'
      : ''
  const desktopLabel = refresh.isPending
    ? 'Syncing…'
    : feedback === 'success'
      ? 'Synced'
      : 'Sync all data'
  const mobileLabel = refresh.isPending
    ? 'Sync…'
    : feedback === 'success'
      ? 'Synced'
      : 'Sync'

  return (
    <>
      <Button
        aria-label="Sync all data"
        className="app-sync-button"
        loading={refresh.isPending}
        onClick={() => refresh.mutate()}
        size="sm"
        title="Pull the latest courses, sessions, and attendance data"
        variant="ghost"
      >
        {!refresh.isPending && <RefreshIcon />}
        <span className="app-sync-button__label">{desktopLabel}</span>
        <span className="app-sync-button__mobile-label">{mobileLabel}</span>
      </Button>
      {feedbackMessage !== '' && (
        <span className="sr-only" role="status">
          {feedbackMessage}
        </span>
      )}
    </>
  )
}
