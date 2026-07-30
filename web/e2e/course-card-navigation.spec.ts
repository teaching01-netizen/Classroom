import { expect, test } from '@playwright/test'
import { mockBackend } from './mock-backend'

for (const viewport of [
  { name: 'desktop', width: 1280, height: 720 },
  { name: 'mobile', width: 375, height: 812 },
] as const) {
  test(`opens sessions when the course card facts are tapped on ${viewport.name}`, async ({ page }) => {
    // Given
    await mockBackend(page)
    await page.setViewportSize(viewport)
    await page.goto('/')
    const courseCard = page.locator('.course-card').first()
    await expect(courseCard).toBeVisible()

    // When
    const factsArea = await courseCard.locator('.course-card__facts').boundingBox()
    if (factsArea === null) throw new Error('Expected the course facts area to be visible')
    await page.mouse.click(factsArea.x + factsArea.width / 2, factsArea.y + factsArea.height / 2)

    // Then
    await expect(page).toHaveURL('/courses/CS101/sessions')
  })
}

test('opens the attendance report from its card action', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/')

  // When
  await page.getByRole('link', { name: 'Attendance report' }).click()

  // Then
  await expect(page).toHaveURL('/courses/CS101/attendance')
})

test('keeps the pin control independent from the card destination', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/')

  // When
  await page.getByRole('button', { name: 'Unpin' }).click()

  // Then
  await expect(page).toHaveURL('/')
})

test('opens sessions from the card link with the keyboard', async ({ page }) => {
  // Given
  await mockBackend(page)
  await page.goto('/')
  const courseLink = page.getByRole('link', { name: /Software Engineering/ })
  await courseLink.focus()

  // When
  await page.keyboard.press('Enter')

  // Then
  await expect(page).toHaveURL('/courses/CS101/sessions')
})
