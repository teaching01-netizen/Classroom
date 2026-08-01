# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: app.spec.ts >> opens the QR dialog and exports session attendance
- Location: e2e/app.spec.ts:21:1

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: getByRole('dialog', { name: 'Student check-in QR code' })
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 5000ms
  - waiting for getByRole('dialog', { name: 'Student check-in QR code' })

```

```yaml
- link "Skip to content":
  - /url: "#main-content"
- banner:
  - link "Check-in Command Center home":
    - /url: /
    - text: Check-in Center
  - navigation "Primary":
    - link "Dashboard":
      - /url: /
    - link "Absence alerts":
      - /url: /absence-dashboard
    - link "All courses":
      - /url: /courses
  - text: Density
  - combobox "Density":
    - option "Comfortable" [selected]
    - option "Compact"
  - text: Live
- main:
  - status "Loading"
```

# Test source

```ts
  1  | import { expect, test } from '@playwright/test'
  2  | import { mockBackend } from './mock-backend'
  3  | 
  4  | test('navigates courses and preserves usable responsive controls', async ({ page }) => {
  5  |   // Given
  6  |   await mockBackend(page)
  7  |   // When
  8  |   await page.goto('/')
  9  |   // Then
  10 |   await expect(page.locator('.app-topbar')).toBeVisible()
  11 |   await expect(page.locator('.app-sidebar')).toHaveCount(0)
  12 |   await page.getByRole('link', { name: 'All courses' }).click()
  13 |   await page.getByRole('searchbox', { name: 'Search courses' }).fill('missing')
  14 |   await expect(page.getByRole('heading', { name: 'All courses' })).toBeVisible()
  15 |   await expect(page.getByText('No courses match these filters.')).toBeVisible()
  16 |   await page.setViewportSize({ width: 390, height: 844 })
  17 |   await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible()
  18 |   await expect(page.getByRole('searchbox', { name: 'Search courses' })).toBeVisible()
  19 | })
  20 | 
  21 | test('opens the QR dialog and exports session attendance', async ({ page }) => {
  22 |   // Given
  23 |   await mockBackend(page)
  24 |   await page.goto('/courses/CS101/sessions/S1')
  25 |   // When
  26 |   const dialog = page.getByRole('dialog', { name: 'Student check-in QR code' })
> 27 |   await expect(dialog).toBeVisible()
     |                        ^ Error: expect(locator).toBeVisible() failed
  28 |   await expect(dialog.getByRole('img', { name: 'QR code for Architecture workshop' })).toBeVisible()
  29 |   await dialog.getByRole('button', { name: 'Close dialog' }).click()
  30 |   const downloadPromise = page.waitForEvent('download')
  31 |   await page.getByRole('button', { name: 'Export CSV' }).click()
  32 |   const download = await downloadPromise
  33 |   // Then
  34 |   expect(download.suggestedFilename()).toBe('checkin_S1.csv')
  35 | })
  36 | 
  37 | test('updates a student optimistically through the live route', async ({ page }) => {
  38 |   // Given
  39 |   await mockBackend(page)
  40 |   await page.goto('/courses/CS101/sessions/S1')
  41 |   await page.getByRole('button', { name: 'Close dialog' }).click()
  42 |   const samRow = page.getByRole('row').filter({ hasText: 'Sam Rivera' })
  43 |   // When
  44 |   await samRow.getByRole('button', { name: 'Check in' }).click()
  45 |   // Then
  46 |   await expect(samRow.getByText('Checked in')).toBeVisible()
  47 |   await expect(page.getByText('Check-in updated.')).toBeVisible()
  48 | })
  49 | 
  50 | test('keeps session content inside the mobile viewport', async ({ page }) => {
  51 |   // Given
  52 |   await mockBackend(page)
  53 |   await page.setViewportSize({ width: 390, height: 844 })
  54 |   // When
  55 |   await page.goto('/courses/CS101/sessions')
  56 |   await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
  57 |   await expect(page.getByLabel('Check-in Command Center home')).toBeVisible()
  58 |   await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible()
  59 |   await expect(page.getByRole('link', { name: 'Absence alerts' })).toBeVisible()
  60 |   await expect(page.getByRole('link', { name: 'All courses' })).toBeVisible()
  61 |   const overflow = await page.locator('body *').evaluateAll((elements) =>
  62 |     elements
  63 |       .filter((element) => element.getBoundingClientRect().right > window.innerWidth + 1)
  64 |       .map((element) => ({
  65 |         className: element.className,
  66 |         tagName: element.tagName,
  67 |         right: Math.round(element.getBoundingClientRect().right),
  68 |       })),
  69 |   )
  70 |   // Then
  71 |   expect(overflow).toEqual([])
  72 | })
  73 | 
```