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
  const identity = JSON.stringify([courseId, sessionId])
  const currentIdentityRef = useRef(identity)
  currentIdentityRef.current = identity

  const [openedIdentity, setOpenedIdentity] = useState<string>()
  const [activeRoom, setActiveRoom] = useState<{ identity: string; roomId: string }>()
  const initializedIdentity = useRef<string>()
  const lastAutoRenewedVersion = useRef<string>()
  const { mutate: startRoomMutate, isPending: startRoomPending } = useStartRoomMutation()
  const activeRoomId = activeRoom?.identity === identity ? activeRoom.roomId : undefined
  const roomQuery = useRoomQuery(activeRoomId, activeRoomId !== undefined)
  const { announce } = useToast()
  const open = openedIdentity === identity

  useEffect(() => {
    if (initializedIdentity.current === identity) {
      return
    }
    initializedIdentity.current = identity
    lastAutoRenewedVersion.current = undefined
    const requestedIdentity = identity

    // Route params can change without unmounting this hook. Never carry a QR
    // room across that identity boundary, even for a single render.
    setActiveRoom(undefined)
    setOpenedIdentity(undefined)

    startRoomMutate(
      { sessionId, courseId },
      {
        onSuccess: (result) => {
          if (currentIdentityRef.current !== requestedIdentity) return
          setActiveRoom({ identity: requestedIdentity, roomId: result.roomId })
          setOpenedIdentity(requestedIdentity)
        },
        onError: (error) => {
          if (currentIdentityRef.current !== requestedIdentity) return
          announce(getErrorMessage(error), 'error')
        },
      },
    )
  }, [announce, courseId, identity, sessionId, startRoomMutate])

  const openQr = () => {
    if (activeRoomId !== undefined) {
      setOpenedIdentity(identity)
      return
    }
    const requestedIdentity = identity
    startRoomMutate(
      { sessionId, courseId },
      {
        onSuccess: (result) => {
          if (currentIdentityRef.current !== requestedIdentity) return
          setActiveRoom({ identity: requestedIdentity, roomId: result.roomId })
          setOpenedIdentity(requestedIdentity)
        },
        onError: (error) => {
          if (currentIdentityRef.current !== requestedIdentity) return
          announce(getErrorMessage(error), 'error')
        },
      },
    )
  }

  const refetchRoom = roomQuery.refetch

  const refresh = useCallback(() => {
    const requestedIdentity = identity
    startRoomMutate(
      { sessionId, courseId },
      {
        onSuccess: () => {
          if (currentIdentityRef.current !== requestedIdentity) return
          void refetchRoom()
        },
        onError: (error) => {
          if (currentIdentityRef.current !== requestedIdentity) return
          announce(getErrorMessage(error), 'error')
        },
      },
    )
  }, [announce, courseId, identity, refetchRoom, sessionId, startRoomMutate])

  // A route transition disables the previous room query immediately. Ignore
  // any data retained by the query observer until the new session owns a room.
  const roomData = activeRoomId === undefined ? undefined : roomQuery.data
  const qrUrl = roomData?.qr_url ?? undefined
  const expiresAt = roomData?.expires_at ?? undefined
  const errorMessage = roomData?.error_message ?? undefined
  const warningMessage = roomData?.warning_message ?? undefined
  const upstreamAttendanceLabel = roomData?.upstream_attendance_label ?? undefined
  const upstreamVerifiedAt = roomData?.upstream_verified_at ?? undefined
  const upstreamVerificationError = roomData?.upstream_verification_error ?? undefined
  const autoRenewVersion = activeRoomId === undefined
    ? undefined
    : JSON.stringify([identity, activeRoomId, expiresAt ?? null])

  const autoRenew = useCallback(() => {
    if (autoRenewVersion === undefined) return
    if (lastAutoRenewedVersion.current === autoRenewVersion) return
    lastAutoRenewedVersion.current = autoRenewVersion
    refresh()
  }, [autoRenewVersion, refresh])

  useEffect(() => {
    if (!open) {
      return
    }
    if (expiresAt === undefined) {
      // A QR without an expiry is anomalous (the backend always pairs them);
      // refresh once to recover rather than showing a frozen image.
      if (qrUrl !== undefined && qrUrl !== '') {
        autoRenew()
      }
      return
    }
    const remaining = new Date(expiresAt).getTime() - Date.now()
    if (remaining <= 0) {
      autoRenew()
      return
    }
    const timer = window.setTimeout(autoRenew, Math.max(remaining - AUTO_REFRESH_AHEAD_MS, 0))
    return () => window.clearTimeout(timer)
  }, [autoRenew, open, expiresAt, qrUrl])

  return {
    qrUrl,
    expiresAt,
    errorMessage,
    warningMessage,
    upstreamAttendanceLabel,
    upstreamVerifiedAt,
    upstreamVerificationError,
    open,
    refreshing: startRoomPending || roomQuery.isFetching,
    openQr,
    closeQr: () => setOpenedIdentity(undefined),
    refresh,
  }
}
