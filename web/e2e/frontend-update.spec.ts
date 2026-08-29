import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

test('reloads the document when a newer frontend deployment is available', async ({ page }) => {
  // Given
  await mockBackend(page)
  let versionChecks = 0
  let documentNavigations = 0
  const versionRequestTokens: string[] = []
  page.on('request', (request) => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame()) {
      documentNavigations += 1
    }
  })
  await page.route('**/version.json?*', async (route) => {
    const requestUrl = new URL(route.request().url())
    const currentBuildId = requestUrl.searchParams.get('current')
    versionRequestTokens.push(requestUrl.searchParams.get('request') ?? '')
    versionChecks += 1
    await route.fulfill({
      json: {
        success: true,
        data: {
          buildId: versionChecks === 1 ? 'new-deployment' : currentBuildId,
        },
      },
    })
  })

  // When
  await page.goto('/courses?filter=active')

  // Then
  await expect.poll(() => documentNavigations).toBe(2)
  await expect.poll(() => versionChecks).toBe(2)
  expect(versionRequestTokens.every((token) => token.length > 0)).toBe(true)
  expect(new Set(versionRequestTokens).size).toBe(2)
  await expect(page).toHaveURL('/courses?filter=active')
  await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
})
