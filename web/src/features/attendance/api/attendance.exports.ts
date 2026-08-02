import { z } from 'zod'
import type { CourseId } from '@/features/courses'
import { ApiError, toApiError } from '@/shared/api/api-error'
import { endpoints } from '@/shared/api/endpoints'

export type AttendanceExportFormat = 'csv' | 'xlsx'

type DownloadAttendanceReportOptions = {
  readonly courseId: CourseId
  readonly format: AttendanceExportFormat
  readonly threshold: number
  readonly signal?: AbortSignal
}

type AttendanceDownload = {
  readonly filename: string
}

const errorEnvelopeSchema = z.object({
  error: z.string().optional(),
  code: z.string().optional(),
  details: z.unknown().optional(),
})

const exportTimeoutMs = 120_000

export async function downloadAttendanceReport(
  options: DownloadAttendanceReportOptions,
): Promise<AttendanceDownload> {
  const timeoutController = new AbortController()
  const timeoutId = window.setTimeout(() => timeoutController.abort(), exportTimeoutMs)
  const signals = [timeoutController.signal]
  if (options.signal !== undefined) {
    signals.push(options.signal)
  }

  let objectUrl: string | undefined
  let anchor: HTMLAnchorElement | undefined
  try {
    const headers = new Headers({
      Accept: options.format === 'csv'
        ? 'text/csv'
        : 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      'Content-Type': 'application/json',
    })
    const response = await fetch(endpoints.attendanceExport(options.courseId), {
      method: 'POST',
      body: JSON.stringify({ format: options.format, threshold: options.threshold }),
      cache: 'no-store',
      headers,
      signal: AbortSignal.any(signals),
    })
    if (!response.ok) {
      throw await attendanceExportError(response)
    }

    const blob = await response.blob()
    const filename = dispositionFilename(response.headers.get('Content-Disposition'))
      ?? `attendance-report.${options.format}`
    objectUrl = URL.createObjectURL(blob)
    anchor = document.createElement('a')
    anchor.href = objectUrl
    anchor.download = filename
    anchor.hidden = true
    document.body.append(anchor)
    anchor.click()
    return { filename }
  } catch (error: unknown) {
    if (timeoutController.signal.aborted && !(options.signal?.aborted ?? false)) {
      throw new ApiError(`The request timed out after ${exportTimeoutMs}ms.`, {
        kind: 'timeout',
        cause: error,
      })
    }
    throw toApiError(error)
  } finally {
    window.clearTimeout(timeoutId)
    anchor?.remove()
    if (objectUrl !== undefined) {
      URL.revokeObjectURL(objectUrl)
    }
  }
}

async function attendanceExportError(response: Response): Promise<ApiError> {
  const text = await response.text()
  let payload: unknown
  try {
    payload = text.length === 0 ? null : JSON.parse(text)
  } catch (error: unknown) {
    if (!(error instanceof SyntaxError)) {
      throw error
    }
    payload = null
  }
  const envelope = errorEnvelopeSchema.safeParse(payload)
  return new ApiError(
    envelope.success && envelope.data.error !== undefined
      ? envelope.data.error
      : `Request failed with status ${response.status}.`,
    {
      kind: 'http',
      status: response.status,
      code: envelope.success ? envelope.data.code : undefined,
      details: envelope.success ? envelope.data.details : payload,
      requestId: response.headers.get('X-Request-ID') ?? undefined,
    },
  )
}

function dispositionFilename(header: string | null): string | undefined {
  if (header === null) {
    return undefined
  }
  const parameters = header.split(';').map((part) => part.trim())
  const encoded = parameters.find((part) => part.toLowerCase().startsWith('filename*='))
  if (encoded !== undefined) {
    const value = encoded.slice(encoded.indexOf('=') + 1).replace(/^UTF-8''/i, '')
    try {
      return safeDownloadFilename(decodeURIComponent(value))
    } catch (error: unknown) {
      if (!(error instanceof URIError)) {
        throw error
      }
    }
  }
  const plain = parameters.find((part) => part.toLowerCase().startsWith('filename='))
  if (plain === undefined) {
    return undefined
  }
  const value = plain.slice(plain.indexOf('=') + 1).replace(/^"|"$/g, '')
  return safeDownloadFilename(value)
}

function safeDownloadFilename(value: string): string | undefined {
  const parts = value.replace(/[\r\n]/g, '').split(/[/\\]/)
  const filename = parts.at(-1)?.trim()
  return filename === undefined || filename.length === 0 ? undefined : filename
}
