import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

const evidenceDirectory = '.omo/evidence/top-navigation'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'mobile', width: 390, height: 844 },
] as const

test.setTimeout(90_000)

test('captures every route and QR state at every responsive breakpoint', async ({ page }) => {
  // Given
  await mockBackend(page)
  // When
  for (const viewport of viewports) {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await captureRoute(page, viewport.name, 'home', '/', 'Your teaching dashboard')
    await captureRoute(page, viewport.name, 'courses', '/courses', 'All courses')
    await captureRoute(
      page,
      viewport.name,
      'sessions',
      '/courses/CS101/sessions',
      'Software Engineering',
    )
    await page.goto('/courses/CS101/sessions/S1')
    const dialog = page.getByRole('dialog', { name: 'Student check-in QR code' })
    await expect(dialog).toBeVisible()
    await page.screenshot({
      path: `${evidenceDirectory}/${viewport.name}/checkin-qr.png`,
    })
    await dialog.getByRole('button', { name: 'Close dialog' }).click()
    await expect(dialog).not.toBeVisible()
    await captureCurrentPage(page, viewport.name, 'checkin')
    await captureRoute(
      page,
      viewport.name,
      'attendance',
      '/courses/CS101/attendance',
      'Software Engineering',
    )
    await captureRoute(
      page,
      viewport.name,
      'absence-dashboard',
      '/absence-dashboard?courses=CS101&threshold=1&load=1',
      'Student absence dashboard',
    )
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/courses')
  await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
  const navigationLink = page.getByRole('link', { name: 'Dashboard' })
  await page.screenshot({
    path: `${evidenceDirectory}/motion/nav-rest.png`,
  })
  await navigationLink.hover()
  await page.waitForTimeout(70)
  await page.screenshot({
    path: `${evidenceDirectory}/motion/nav-mid.png`,
  })
  await page.waitForTimeout(160)
  await page.screenshot({
    path: `${evidenceDirectory}/motion/nav-settled.png`,
  })
  // Then
  await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
})

async function captureRoute(
  page: import('@playwright/test').Page,
  viewport: string,
  name: string,
  path: string,
  heading: string,
): Promise<void> {
  await page.goto(path)
  await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  await captureCurrentPage(page, viewport, name)
}

async function captureCurrentPage(
  page: import('@playwright/test').Page,
  viewport: string,
  name: string,
): Promise<void> {
  await page.evaluate(() => {
    window.scrollTo(0, 0)
    document.scrollingElement?.scrollTo(0, 0)
  })
  await expect.poll(() => page.evaluate(() => [window.scrollX, window.scrollY])).toEqual([0, 0])
  await expect(page.locator('.app-topbar')).toBeVisible()
  await page.waitForTimeout(100)
  await page.screenshot({
    path: `${evidenceDirectory}/${viewport}/${name}.png`,
  })
  const hasBelowFoldContent = await page.evaluate(
    () => document.documentElement.scrollHeight > window.innerHeight + 1,
  )
  if (hasBelowFoldContent) {
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    await expect(page.locator('.app-topbar')).toBeVisible()
    await page.waitForTimeout(100)
    await page.screenshot({
      path: `${evidenceDirectory}/${viewport}/${name}-bottom.png`,
    })
  }
}
