# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: app.spec.ts >> updates a student optimistically through the live route
- Location: e2e/app.spec.ts:37:1

# Error details

```
Test timeout of 30000ms exceeded.
```

```
Error: locator.click: Test timeout of 30000ms exceeded.
Call log:
  - waiting for getByRole('button', { name: 'Close dialog' })

```

# Page snapshot

```yaml
- generic [ref=e3]:
  - link "Skip to content" [ref=e4] [cursor=pointer]:
    - /url: "#main-content"
  - banner [ref=e5]:
    - link "Check-in Command Center home" [ref=e6] [cursor=pointer]:
      - /url: /
      - generic [ref=e7]: C
      - generic [ref=e8]: Check-in Center
    - navigation "Primary" [ref=e9]:
      - link "Dashboard" [ref=e10] [cursor=pointer]:
        - /url: /
      - link "Absence alerts" [ref=e11] [cursor=pointer]:
        - /url: /absence-dashboard
      - link "All courses" [ref=e12] [cursor=pointer]:
        - /url: /courses
    - generic [ref=e13]:
      - generic [ref=e14]:
        - generic [ref=e15]: Density
        - combobox "Density" [ref=e16]:
          - option "Comfortable" [selected]
          - option "Compact"
      - generic [ref=e17]: Live
  - main [ref=e19]:
    - generic [ref=e20]:
      - link "← Back to sessions" [ref=e21] [cursor=pointer]:
        - /url: /courses/CS101/sessions
      - generic [ref=e22]:
        - generic [ref=e23]:
          - paragraph [ref=e24]: Live check-in
          - heading "Architecture workshop" [level=2] [ref=e25]
          - paragraph [ref=e26]: Software Engineering
        - generic [ref=e27]:
          - button "View QR code" [disabled] [ref=e28]
          - button "Export CSV" [ref=e31] [cursor=pointer]
      - generic [ref=e33]:
        - generic [ref=e34]:
          - term [ref=e35]: Checked in
          - definition [ref=e36]: 1/2
        - generic [ref=e37]:
          - term [ref=e38]: Attendance rate
          - definition [ref=e39]: 50%
      - generic [ref=e40]:
        - generic [ref=e41]:
          - generic [ref=e42]: Search students
          - searchbox "Search students" [ref=e43]
        - generic [ref=e44]:
          - generic [ref=e45]: Check-in status
          - combobox "Check-in status" [ref=e46]:
            - option "All students" [selected]
            - option "Checked in"
            - option "Not checked in"
      - table [ref=e48]:
        - rowgroup [ref=e49]:
          - row [ref=e50]:
            - columnheader "Student" [ref=e51]
            - columnheader "School" [ref=e52]
            - columnheader "Status" [ref=e53]
            - columnheader "Points" [ref=e54]
            - columnheader "Action" [ref=e55]
        - rowgroup [ref=e57]:
          - row [ref=e58]:
            - cell "Alice u123" [ref=e59]:
              - generic [ref=e60]:
                - generic [ref=e61]: AC
                - generic [ref=e62]:
                  - strong [ref=e63]: Alice
                  - generic [ref=e64]: u123
            - cell "Computer Science" [ref=e65]
            - cell "Checked in" [ref=e66]
            - cell "4" [ref=e68]
            - cell [ref=e69]:
              - button "Undo" [ref=e70] [cursor=pointer]
          - row [ref=e72]:
            - cell "Sam Rivera u456" [ref=e73]:
              - generic [ref=e74]:
                - generic [ref=e75]: SR
                - generic [ref=e76]:
                  - strong [ref=e77]: Sam Rivera
                  - generic [ref=e78]: u456
            - cell "Computer Science" [ref=e79]
            - cell "Not checked in" [ref=e80]
            - cell "1" [ref=e82]
            - cell [ref=e83]:
              - button "Check in" [ref=e84] [cursor=pointer]
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
  27 |   await expect(dialog).toBeVisible()
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
> 41 |   await page.getByRole('button', { name: 'Close dialog' }).click()
     |                                                            ^ Error: locator.click: Test timeout of 30000ms exceeded.
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