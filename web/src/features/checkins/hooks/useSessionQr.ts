import { useCallback, useEffect, useRef, useState } from 'react'
import { useRoomQuery, useStartRoomMutation } from '@/features/rooms'
import type { CourseId } from '@/features/courses'
import type { SessionId } from '@/features/sessions'
import { getErrorMessage } from '@/shared/lib/errors'
import { useToast } from '@/shared/ui/Toast'

// Refresh shortly before the QR expires so students never scan a stale code.
// Mirrors the external Warwick site, which reloads the QR when its countdown
// reaches zero. The backend worker already keeps the room's QR fresh, so this
// refetch is a cheap read of the current room state.
const AUTO_REFRESH_AHEAD_MS = 10_000

// useSessionQr owns the QR-room flow for a session independently of the roster
// query. On mount it issues a single idempotent
// POST /api/rooms/from-session/start (find-or-create-and-start) and polls the
// room detail with an adaptive interval until qr_url arrives. While the dialog
// is open it keeps the displayed QR fresh by re-fetching shortly before expiry.
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

  const refresh = useCallback(() => {
    startRoom.mutate(
      { sessionId, courseId },
      {
        onSuccess: () => {
          void roomQuery.refetch()
        },
        onError: (error) => announce(getErrorMessage(error), 'error'),
      },
    )
  }, [announce, courseId, roomQuery, sessionId, startRoom])

  const qrUrl = roomQuery.data?.qr_url ?? undefined
  const expiresAt = roomQuery.data?.expires_at ?? undefined

  useEffect(() => {
    if (!open) {
      return
    }
    if (expiresAt === undefined) {
      // A QR without an expiry is anomalous (the backend always pairs them);
      // refresh once to recover rather than showing a frozen image.
      if (qrUrl !== undefined && qrUrl !== '') {
        refresh()
      }
      return
    }
    const remaining = new Date(expiresAt).getTime() - Date.now()
    if (remaining <= 0) {
      refresh()
      return
    }
    const timer = window.setTimeout(refresh, Math.max(remaining - AUTO_REFRESH_AHEAD_MS, 0))
    return () => window.clearTimeout(timer)
  }, [open, expiresAt, qrUrl, refresh])

  return {
    qrUrl,
    expiresAt,
    open,
    refreshing: startRoom.isPending || roomQuery.isFetching,
    openQr,
    closeQr: () => setOpen(false),
    refresh,
  }
}
