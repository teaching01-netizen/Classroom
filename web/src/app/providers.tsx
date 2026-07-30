import { QueryClientProvider } from '@tanstack/react-query'
import { useEffect, useMemo, type ReactNode } from 'react'
import { queryClient } from './query-client'
import { RealtimeClient } from '@/shared/realtime/websocket-client'
import { ToastProvider } from '@/shared/ui/Toast'

type AppProvidersProps = {
  readonly children: ReactNode
}

export function AppProviders({ children }: AppProvidersProps) {
  const realtimeClient = useMemo(() => new RealtimeClient(queryClient), [])

  useEffect(() => {
    realtimeClient.start()
    return () => realtimeClient.stop()
  }, [realtimeClient])

  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>{children}</ToastProvider>
    </QueryClientProvider>
  )
}
