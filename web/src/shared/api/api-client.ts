import { z } from 'zod'
import { ApiError, toApiError } from './api-error'

type QueryValue = string | number | boolean | null | undefined

type RequestOptions<T> = Omit<RequestInit, 'body'> & {
  readonly schema: z.ZodType<T>
  readonly body?: unknown
  readonly query?: Readonly<Record<string, QueryValue>>
  readonly timeoutMs?: number
}

const envelopeSchema = z.object({
  success: z.boolean(),
  data: z.unknown().optional(),
  error: z.string().optional(),
  code: z.string().optional(),
  details: z.unknown().optional(),
})

function withQuery(path: string, query: Readonly<Record<string, QueryValue>> | undefined): string {
  if (query === undefined) {
    return path
  }
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null) {
      params.set(key, String(value))
    }
  }
  const serialized = params.toString()
  return serialized.length === 0 ? path : `${path}?${serialized}`
}

function requestHeaders(headers: HeadersInit | undefined, hasBody: boolean): Headers {
  const result = new Headers(headers)
  result.set('Accept', 'application/json')
  result.set('X-Request-ID', crypto.randomUUID())
  if (hasBody && !result.has('Content-Type')) {
    result.set('Content-Type', 'application/json')
  }
  return result
}

async function readPayload(response: Response): Promise<unknown> {
  const text = await response.text()
  if (text.length === 0) {
    return null
  }
  try {
    return JSON.parse(text)
  } catch (error: unknown) {
    throw new ApiError('The server returned invalid JSON.', {
      kind: 'invalid-json',
      status: response.status,
      requestId: response.headers.get('X-Request-ID') ?? undefined,
      cause: error,
    })
  }
}

async function request<T>(path: string, options: RequestOptions<T>): Promise<T> {
  const timeoutController = new AbortController()
  const timeoutMs = options.timeoutMs ?? 30_000
  const timeoutId = window.setTimeout(() => timeoutController.abort('timeout'), timeoutMs)
  const signals = [timeoutController.signal]
  if (options.signal !== undefined && options.signal !== null) {
    signals.push(options.signal)
  }

  const hasBody = options.body !== undefined
  const {
    body,
    query,
    schema,
    timeoutMs: _timeoutMs,
    headers,
    signal: _signal,
    ...requestInit
  } = options

  try {
    const bodyInit: Pick<RequestInit, 'body'> | object = hasBody
      ? { body: JSON.stringify(body) }
      : {}
    const response = await fetch(withQuery(path, query), {
      ...requestInit,
      ...bodyInit,
      cache: 'no-store',
      headers: requestHeaders(headers, hasBody),
      signal: AbortSignal.any(signals),
    })
    const payload = await readPayload(response)
    const envelope = envelopeSchema.safeParse(payload)
    const requestId = response.headers.get('X-Request-ID') ?? undefined

    if (!response.ok) {
      throw new ApiError(
        envelope.success && envelope.data.error !== undefined
          ? envelope.data.error
          : `Request failed with status ${response.status}.`,
        {
          kind: 'http',
          status: response.status,
          code: envelope.success ? envelope.data.code : undefined,
          details: envelope.success ? envelope.data.details : payload,
          requestId,
        },
      )
    }
    if (!envelope.success) {
      throw new ApiError('The server returned an invalid response envelope.', {
        kind: 'invalid-response',
        status: response.status,
        details: envelope.error.issues,
        requestId,
      })
    }
    if (!envelope.data.success) {
      throw new ApiError(envelope.data.error ?? 'The request could not be completed.', {
        kind: 'http',
        status: response.status,
        code: envelope.data.code,
        details: envelope.data.details,
        requestId,
      })
    }
    return schema.parse(envelope.data.data)
  } catch (error: unknown) {
    if (timeoutController.signal.aborted && !(options.signal?.aborted ?? false)) {
      throw new ApiError(`The request timed out after ${timeoutMs}ms.`, {
        kind: 'timeout',
        cause: error,
      })
    }
    throw toApiError(error)
  } finally {
    window.clearTimeout(timeoutId)
  }
}

export const apiClient = {
  get<T>(path: string, options: Omit<RequestOptions<T>, 'method' | 'body'>): Promise<T> {
    return request(path, { ...options, method: 'GET' })
  },
  post<T>(path: string, options: Omit<RequestOptions<T>, 'method'>): Promise<T> {
    return request(path, { ...options, method: 'POST' })
  },
  put<T>(path: string, options: Omit<RequestOptions<T>, 'method'>): Promise<T> {
    return request(path, { ...options, method: 'PUT' })
  },
  delete<T>(path: string, options: Omit<RequestOptions<T>, 'method'>): Promise<T> {
    return request(path, { ...options, method: 'DELETE' })
  },
} as const
