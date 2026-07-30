import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

for (const viewport of [
  { name: 'desktop', width: 1280, height: 720 },
  { name: 'mobile', width: 375, height: 812 },
] as const) {
  test(`opens a session from its status area on ${viewport.name}`, async ({ page }) => {
    // Given
    await mockBackend(page)
    await page.setViewportSize(viewport)
    await page.goto('/courses/CS101/sessions')

    // When
    const statusArea = await page.getByRole('row', { name: /Architecture workshop/ }).locator('td').nth(2).boundingBox()
    if (statusArea === null) throw new Error('Expected the session status area to be visible')
    await page.mouse.click(statusArea.x + statusArea.width / 2, statusArea.y + statusArea.height / 2)

    // Then
    await expect(page).toHaveURL('/courses/CS101/sessions/S1')
  })
}
