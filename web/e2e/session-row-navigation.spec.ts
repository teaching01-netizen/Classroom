import { expect, test, type Locator, type Page } from '@playwright/test'
import { mockBackend } from './mock-backend'

const sessions = [
  { session_id: 'S1', session_number: 1, name: 'Architecture workshop', date: '2026-02-01', checked_in_count: 1, total_students: 2, status: 'active' },
  { session_id: 'S2', session_number: 2, name: 'API design clinic', date: '2026-02-08', checked_in_count: 2, total_students: 2, status: 'done' },
  { session_id: 'S3', session_number: 3, name: 'Data review', date: '2026-02-15', checked_in_count: 0, total_students: 2, status: 'not_started' },
] as const

async function activate(locator: Locator, hasTouch: boolean): Promise<void> {
  if (hasTouch) {
    await locator.tap()
    return
  }
  await locator.click()
}

async function installSessionListFixture(page: Page): Promise<void> {
  await mockBackend(page)
  await page.route('**/api/teacher/courses/CS101', async (route) => {
    await route.fulfill({
      json: {
        success: true,
        data: {
          course_id: 'CS101',
          name: 'Software Engineering',
          start_date: '2026-01-10',
          end_date: '2026-04-10',
          enrolled_count: 2,
          total_sessions: sessions.length,
          completed_sessions: 1,
          avg_attendance_rate: 0.5,
          status: 'active',
          sessions,
        },
      },
    })
  })
}

async function returnToSessionList(page: Page, sessionName: string): Promise<void> {
  await page.goBack()
  await expect(page.getByRole('row', { name: new RegExp(sessionName) })).toBeVisible()
}

test('each session row opens its own attendance page', async ({ page }, testInfo) => {
  await installSessionListFixture(page)
  await page.goto('/courses/CS101/sessions')
  const hasTouch = testInfo.project.use.hasTouch === true

  for (const session of sessions) {
    const row = page.getByRole('row', { name: new RegExp(session.name) })
    await activate(row.locator('td').nth(2), hasTouch)
    await expect(page).toHaveURL(`/courses/CS101/sessions/${session.session_id}`)
    await returnToSessionList(page, session.name)
  }
})

test('session name and Open links keep the destination scoped to their row', async ({ page }, testInfo) => {
  await installSessionListFixture(page)
  await page.goto('/courses/CS101/sessions')
  const hasTouch = testInfo.project.use.hasTouch === true

  const middleRow = page.getByRole('row', { name: /API design clinic/ })
  const sessionNameLink = middleRow.locator('a.session-card-link')
  await expect(sessionNameLink).toHaveAttribute('href', '/courses/CS101/sessions/S2')
  await activate(sessionNameLink, hasTouch)
  await expect(page).toHaveURL('/courses/CS101/sessions/S2')
  await returnToSessionList(page, 'API design clinic')

  const lastRow = page.getByRole('row', { name: /Data review/ })
  const openLink = lastRow.getByRole('link', { name: 'Open Data review', exact: true })
  await expect(openLink).toHaveAttribute('href', '/courses/CS101/sessions/S3')
  await activate(openLink, hasTouch)
  await expect(page).toHaveURL('/courses/CS101/sessions/S3')
})
