import { ZodError } from 'zod'

export const apiErrorKinds = [
  'http',
  'invalid-json',
  'invalid-response',
  'network',
  'timeout',
  'cancelled',
] as const

export type ApiErrorKind = (typeof apiErrorKinds)[number]

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly status: number
  readonly code: string | undefined
  readonly details: unknown
  readonly requestId: string | undefined

  constructor(
    message: string,
    options: {
      readonly kind: ApiErrorKind
      readonly status?: number | undefined
      readonly code?: string | undefined
      readonly details?: unknown
      readonly requestId?: string | undefined
      readonly cause?: unknown
    },
  ) {
    super(message, options.cause === undefined ? undefined : { cause: options.cause })
    this.name = 'ApiError'
    this.kind = options.kind
    this.status = options.status ?? 0
    this.code = options.code
    this.details = options.details
    this.requestId = options.requestId
  }
}

export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error
  }
  if (error instanceof ZodError) {
    return new ApiError('The server returned data in an unexpected format.', {
      kind: 'invalid-response',
      details: error.issues,
      cause: error,
    })
  }
  if (error instanceof DOMException && error.name === 'AbortError') {
    return new ApiError('The request was cancelled.', {
      kind: 'cancelled',
      cause: error,
    })
  }
  if (error instanceof Error) {
    return new ApiError(error.message || 'A network error occurred.', {
      kind: 'network',
      cause: error,
    })
  }
  return new ApiError('An unexpected network error occurred.', {
    kind: 'network',
    details: error,
  })
}
