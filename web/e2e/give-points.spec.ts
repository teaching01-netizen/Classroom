import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

async function openRoster(page: import('@playwright/test').Page) {
  await page.goto('/courses/CS101/sessions/S1')
  const closeDialog = page.getByRole('button', { name: 'Close dialog' })
  await expect(closeDialog).toBeVisible()
  await closeDialog.click()
}

// Rows are located by the student id shown in the row's secondary line:
// Alice's row renders her nickname ("Alice"), not her full name.
function studentRow(page: import('@playwright/test').Page, studentId: string) {
  return page.getByRole('row').filter({ hasText: studentId })
}

test('eligible rows offer a control and ineligible rows do not', async ({ page }) => {
  await mockBackend(page)
  await openRoster(page)

  const aliceRow = studentRow(page, 'u123')
  const samRow = studentRow(page, 'u456')
  const jordanRow = studentRow(page, 'u789')

  await expect(aliceRow.getByRole('button', { name: 'Give points to Alice Chen' })).toBeVisible()
  // Sam holds 1 point but Warwick says ineligible: scoring must not gate the control.
  await expect(samRow.getByRole('button', { name: /^Give points to / })).toHaveCount(0)
  // Jordan is checked out but eligible: check-in status must not gate the control.
  await expect(jordanRow.getByRole('button', { name: 'Give points to Jordan Lee' })).toBeVisible()
})

test('awards points, names the student in the dialog, and converges the row total', async ({ page }) => {
  const backend = await mockBackend(page)
  await openRoster(page)
  const aliceRow = studentRow(page, 'u123')
  const alicePoints = aliceRow.getByRole('cell').nth(3)
  await expect(alicePoints).toHaveText('4')

  await aliceRow.getByRole('button', { name: 'Give points to Alice Chen' }).click()

  const dialog = page.getByRole('dialog', { name: 'Give participation points' })
  await expect(dialog).toContainText('Alice Chen')
  await dialog.getByLabel('Points').fill('2')
  await dialog.getByRole('button', { name: 'Award points' }).click()

  await expect(dialog).toBeHidden()
  await expect(alicePoints).toHaveText('6')
  await expect(page.getByText('2 point(s) awarded to Alice Chen.')).toBeVisible()
  expect(backend.pointsRequests[0]).toMatchObject({
    studentId: 'u123',
    points: 2,
    expectedPoints: 4,
  })
})

test('returns focus to the originating row button after the dialog closes', async ({ page }) => {
  await mockBackend(page)
  await openRoster(page)
  const button = page.getByRole('button', { name: 'Give points to Jordan Lee' })

  await button.click()
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()

  await expect(button).toBeFocused()
})

test('a Humanix rejection surfaces and leaves the total untouched', async ({ page }) => {
  await mockBackend(page, { pointsMode: 'failure' })
  await openRoster(page)
  const jordanRow = studentRow(page, 'u789')
  const jordanPoints = jordanRow.getByRole('cell').nth(3)

  await jordanRow.getByRole('button', { name: 'Give points to Jordan Lee' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Award points' }).click()

  await expect(page.getByText(/Student did not check in/).first()).toBeVisible()
  await expect(jordanPoints).toHaveText('0')
})
