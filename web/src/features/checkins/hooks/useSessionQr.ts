import { useEffect, useRef, useState } from 'react'
import { useRoomQuery, useStartRoomMutation } from '@/features/rooms'
import type { CourseId } from '@/features/courses'
import type { SessionId } from '@/features/sessions'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'

// useSessionQr owns the QR-room flow for a session independently of the roster
// query. On mount it issues a single idempotent
// POST /api/rooms/from-session/start (find-or-create-and-start) and polls the
// room detail with an adaptive interval until qr_url arrives.
export function useSessionQr(courseId: CourseId, sessionId: SessionId) {
  const [open, setOpen] = useState(false)
  const [activeRoomId, setActiveRoomId] = useState<string>()
  const initializedRoom = useRef(false)
  const startRoom = useStartRoomMutation()
  const roomQuery = useRoomQuery(activeRoomId, activeRoomId !== undefined)
  const { announce } = useToast()

  useEffect(() => {
    if (initializedRoom.current) {
      return
    }
    initializedRoom.current = true
    startRoom.mutate(
      { sessionId, courseId },
      {
        onSuccess: (result) => {
          setActiveRoomId(result.roomId)
          setOpen(true)
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }, [announce, courseId, sessionId, startRoom])

  const openQr = () => {
    if (activeRoomId !== undefined) {
      setOpen(true)
      return
    }
    startRoom.mutate(
      { sessionId, courseId },
      {
        onSuccess: (result) => {
          setActiveRoomId(result.roomId)
          setOpen(true)
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }

  const refresh = () => {
    startRoom.mutate(
      { sessionId, courseId },
      {
        onSuccess: () => {
          void roomQuery.refetch()
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }

  return {
    qrUrl: roomQuery.data?.qr_url ?? undefined,
    open,
    refreshing: startRoom.isPending || roomQuery.isFetching,
    openQr,
    closeQr: () => setOpen(false),
    refresh,
  }
}
