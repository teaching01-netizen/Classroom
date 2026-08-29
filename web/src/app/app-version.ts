import { z } from 'zod'
import { apiClient } from '@/shared/api/api-client'
import { ApiError } from '@/shared/api/api-error'

export type AppVersionCheckResult = 'current' | 'reloading' | 'unavailable'

const reloadMarker = '__app_version'
const buildVersionSchema = z.object({
  buildId: z.string().min(1),
})

type BuildVersion = z.infer<typeof buildVersionSchema>

export type AppVersionRuntime = {
  readonly cleanUrl: (url: string) => void
  readonly currentBuildId: string
  readonly currentUrl: () => string
  readonly readLatestBuild: () => Promise<BuildVersion | null>
  readonly replaceUrl: (url: string) => void
}

export async function checkForAppUpdate(
  runtime: AppVersionRuntime,
): Promise<AppVersionCheckResult> {
  const latestBuild = await runtime.readLatestBuild()
  if (latestBuild === null) {
    return 'unavailable'
  }

  const currentUrl = new URL(runtime.currentUrl())
  if (latestBuild.buildId !== runtime.currentBuildId) {
    currentUrl.searchParams.set(reloadMarker, latestBuild.buildId)
    runtime.replaceUrl(currentUrl.toString())
    return 'reloading'
  }

  if (currentUrl.searchParams.has(reloadMarker)) {
    currentUrl.searchParams.delete(reloadMarker)
    runtime.cleanUrl(currentUrl.toString())
  }

  return 'current'
}

export const browserAppVersionRuntime: AppVersionRuntime = {
  cleanUrl: (url) => window.history.replaceState(window.history.state, '', url),
  currentBuildId: import.meta.env['VITE_APP_BUILD_ID'],
  currentUrl: () => window.location.href,
  readLatestBuild: async () => {
    try {
      return await apiClient.get('/version.json', {
        query: {
          current: import.meta.env['VITE_APP_BUILD_ID'],
          request: crypto.randomUUID(),
        },
        schema: buildVersionSchema,
        timeoutMs: 5_000,
      })
    } catch (error: unknown) {
      if (error instanceof ApiError) {
        return null
      }
      throw error
    }
  },
  replaceUrl: (url) => window.location.replace(url),
}
