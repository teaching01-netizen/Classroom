import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

test('navigates courses and preserves usable responsive controls', async ({ page }) => {
  // Given
  await mockBackend(page)
  // When
  await page.goto('/')
  // Then
  await expect(page.locator('.app-topbar')).toBeVisible()
  await expect(page.locator('.app-sidebar')).toHaveCount(0)
  await page.getByRole('link', { name: 'All courses' }).click()
  await page.getByRole('searchbox', { name: 'Search courses' }).fill('missing')
  await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
  await expect(page.getByText('No courses match these filters.')).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible()
  await expect(page.getByRole('searchbox', { name: 'Search courses' })).toBeVisible()
})

test('syncs the fresh catalog and shows newly added courses', async ({ page }) => {
  await mockBackend(page)
  await page.goto('/courses')

  await expect(page.getByRole('heading', { name: 'Software Engineering' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Distributed Systems' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Sync all data' }).click()

  await expect(page.getByRole('heading', { name: 'Distributed Systems' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sync all data' })).toContainText('Synced')
})

test('opens the QR dialog and exports session attendance', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/courses/CS101/sessions/S1')
  // When
  const dialog = page.getByRole('dialog', { name: 'Student check-in QR code' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('img', { name: 'QR code for Architecture workshop' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Close dialog' }).click()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export CSV' }).click()
  const download = await downloadPromise
  // Then
  expect(download.suggestedFilename()).toBe('checkin_S1.csv')
})

test('keeps session content inside the mobile viewport', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.setViewportSize({ width: 390, height: 844 })
  // When
  await page.goto('/courses/CS101/sessions')
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
  await expect(page.getByLabel('Check-in Command Center home')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Absence alerts' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'All courses' })).toBeVisible()
  const overflow = await page.locator('body *').evaluateAll((elements) =>
    elements
      .filter((element) => element.getBoundingClientRect().right > window.innerWidth + 1)
      .map((element) => ({
        className: element.className,
        tagName: element.tagName,
        right: Math.round(element.getBoundingClientRect().right),
      })),
  )
  // Then
  expect(overflow).toEqual([])
})
