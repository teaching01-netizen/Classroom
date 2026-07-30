import { ApiError } from '@/shared/api/api-error'

export function getErrorMessage(error: unknown): string {
  if (error instanceof ApiError || error instanceof Error) {
    return error.message
  }
  return 'An unexpected error occurred.'
}
