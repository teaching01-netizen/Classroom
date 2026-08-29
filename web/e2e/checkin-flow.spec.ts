import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

async function openRoster(page: import('@playwright/test').Page) {
  await page.goto('/courses/CS101/sessions/S1')
  const closeDialog = page.getByRole('button', { name: 'Close dialog' })
  await expect(closeDialog).toBeVisible()
  await closeDialog.click()
}

test('staff can check in and undo the same student through verified Humanix reads', async ({ page }) => {
  const backend = await mockBackend(page)
  await openRoster(page)
  const samRow = page.getByRole('row').filter({ hasText: 'Sam Rivera' })

  await samRow.getByRole('button', { name: 'Check in' }).click()

  await expect(samRow.getByText('Checked in')).toBeVisible()
  await expect(samRow.getByRole('button', { name: 'Undo check-in' })).toBeVisible()
  await expect(page.getByText('Check-in confirmed in Humanix.')).toBeVisible()
  expect(backend.checkinRequests[0]).toMatchObject({
    studentId: 'u456',
    checkedIn: true,
    expectedSnapshotVersion: 7,
  })
  expect(backend.checkinRequests[0]?.idempotencyKey).toMatch(/^[0-9a-f-]{36}$/)

  await samRow.getByRole('button', { name: 'Undo check-in' }).click()

  await expect(samRow.getByText('Not checked in')).toBeVisible()
  await expect(samRow.getByRole('button', { name: 'Check in' })).toBeVisible()
  await expect(page.getByText('Check-in removed from Humanix.')).toBeVisible()
  expect(backend.checkinRequests[1]).toMatchObject({
    studentId: 'u456',
    checkedIn: false,
    expectedSnapshotVersion: 7,
  })
})

test('a rejected Humanix write restores the student row', async ({ page }) => {
  await mockBackend(page, { checkinMode: 'failure' })
  await openRoster(page)
  const samRow = page.getByRole('row').filter({ hasText: 'Sam Rivera' })

  await samRow.getByRole('button', { name: 'Check in' }).click()

  await expect(samRow.getByText('Not checked in')).toBeVisible()
  await expect(samRow.getByRole('button', { name: 'Check in' })).toBeVisible()
  await expect(page.getByText(/Humanix rejected the check-in change.*restored/)).toBeVisible()
})

test('a QR-originated Humanix change refreshes the matching WCode row', async ({ page }) => {
  const backend = await mockBackend(page)
  await openRoster(page)
  const samRow = page.getByRole('row').filter({ hasText: 'Sam Rivera' })
  await expect(samRow.getByText('Not checked in')).toBeVisible()

  backend.qrCheckin('u456', true)

  await expect(samRow.getByText('Checked in')).toBeVisible()
  await expect(samRow.getByRole('button', { name: 'Undo check-in' })).toBeVisible()
})
