import { describe, expect, it, vi } from 'vitest'
import { checkForAppUpdate, type AppVersionRuntime } from './app-version'

function createRuntime(overrides: Partial<AppVersionRuntime> = {}): AppVersionRuntime {
  return {
    cleanUrl: vi.fn(),
    currentBuildId: 'build-1',
    currentUrl: () => 'https://example.test/courses?filter=active',
    readLatestBuild: async () => ({ buildId: 'build-1' }),
    replaceUrl: vi.fn(),
    ...overrides,
  }
}

describe('app version check', () => {
  it('replaces the document URL when a newer frontend build is available', async () => {
    // Given
    const replaceUrl = vi.fn()
    const runtime = createRuntime({
      readLatestBuild: async () => ({ buildId: 'build-2' }),
      replaceUrl,
    })

    // When
    const result = await checkForAppUpdate(runtime)

    // Then
    expect(result).toBe('reloading')
    expect(replaceUrl).toHaveBeenCalledWith(
      'https://example.test/courses?filter=active&__app_version=build-2',
    )
  })

  it('removes the reload marker after the new build starts', async () => {
    // Given
    const cleanUrl = vi.fn()
    const runtime = createRuntime({
      cleanUrl,
      currentBuildId: 'build-2',
      currentUrl: () =>
        'https://example.test/courses?filter=active&__app_version=build-2',
      readLatestBuild: async () => ({ buildId: 'build-2' }),
    })

    // When
    const result = await checkForAppUpdate(runtime)

    // Then
    expect(result).toBe('current')
    expect(cleanUrl).toHaveBeenCalledWith(
      'https://example.test/courses?filter=active',
    )
  })

  it('keeps the current UI when the version response is unavailable', async () => {
    // Given
    const replaceUrl = vi.fn()
    const runtime = createRuntime({
      readLatestBuild: async () => null,
      replaceUrl,
    })

    // When
    const result = await checkForAppUpdate(runtime)

    // Then
    expect(result).toBe('unavailable')
    expect(replaceUrl).not.toHaveBeenCalled()
  })
})
